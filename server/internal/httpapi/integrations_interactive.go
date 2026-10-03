package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/actions"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Interactive messages: answering a press.

A press already becomes an event in the room. What is here is everything after
it — a program replacing the message's controls where everybody sees them,
answering the person who pressed and nobody else, and remembering where it got
to in an exchange with somebody between one step and the next.
*/

func init() {
	registerRoutes(func(s *Server) {
		rooms := s.echo.Group("/api/rooms", requireAuthentication)

		/*
			Replacing a message's text and controls.

			PUT, because the request carries the whole new state rather than
			a change to it; and conditional on the version it names, so two
			answers to one press cannot both land.
		*/
		rooms.PUT("/:id/messages/:eventId/controls", s.handleUpdateControls)

		/*
			Answering one press privately.

			Addressed by the press, never by a person: the program can reach
			exactly the somebody who asked, for a short while after they
			asked, and nobody else.
		*/
		rooms.POST("/:id/presses/:eventId/reply", s.handlePrivateReply)

		/*
			What a program remembers about an exchange with one person.

			The program is always the caller, so there is no request by which
			anybody reads what a different program remembers — or what a
			program remembers about somebody else.
		*/
		rooms.GET("/:id/context/:userId", s.handleReadContext)
		rooms.PUT("/:id/context/:userId", s.handleRememberContext)
		rooms.DELETE("/:id/context/:userId", s.handleForgetContext)
	})

	documentRoutes(map[string]entry{
		"PUT /api/rooms/:id/messages/:eventId/controls": {
			Summary: "Replace a message's text and controls", Authentication: "session or token",
		},
		"POST /api/rooms/:id/presses/:eventId/reply": {
			Summary: "Answer a press privately", Authentication: "session or token",
		},
		"GET /api/rooms/:id/context/:userId": {
			Summary: "Read what an integration remembers", Authentication: "session or token",
		},
		"PUT /api/rooms/:id/context/:userId": {
			Summary: "Remember something about an exchange", Authentication: "session or token",
		},
		"DELETE /api/rooms/:id/context/:userId": {
			Summary: "Forget an exchange", Authentication: "session or token",
		},
	})
}

// pressRequest is what a press may carry. All of it is optional: a button
// pressed by a client that predates menus sends nothing at all.
type pressRequest struct {
	Value string `json:"value"`
	// Version is the version of the controls the person was looking at.
	Version *int32 `json:"version"`
}

/*
pressInteractive records a press of any control.

Throttled twice before anything is read or written: per person, so nobody can
hammer a channel's buttons, and per program, so a program whose question
reached a very large channel is not handed more answers than it — or this
server, which carries out whatever it does in response — can take.
*/
func (s *Server) pressInteractive(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request pressRequest
	if raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 16<<10)); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &request); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
		}
	}

	if refusal := s.throttlePress(c, "press:user:"+user.ID.String(), settings.PressesPerUserPerMinute); refusal != nil {
		return refusal
	}

	/*
		eventIDParam rather than c.Param: an event identifier begins with "$",
		which a correct client percent-encodes, and Echo hands back the raw
		segment. Reading it directly yields "%24abc" and every lookup misses —
		invisibly, for as long as every client leaves the "$" alone.
	*/
	messageID := eventIDParam(c)
	message, err := s.rooms.Event(ctx, messageID)
	if err == nil && message.SenderID != nil {
		if refusal := s.throttlePress(c, "press:integration:"+message.SenderID.String(),
			settings.PressesPerIntegrationPerMinute); refusal != nil {
			return refusal
		}
	}

	permissions := currentPermissions(c)
	event, err := s.rooms.PressAction(ctx, rooms.Press{
		RoomID:  roomID,
		UserID:  user.ID,
		Message: messageID,
		Action:  c.Param("action"),
		Value:   request.Value,
		Version: request.Version,
		Permits: func(permission string) bool {
			return permissions.AllowsChannel(authz.Permission(permission), roomID).Allowed
		},
	})
	if err != nil {
		return interactiveError(err)
	}

	s.publishEvent(ctx, event)
	s.queueInvocation(ctx, message, event)
	return c.JSON(http.StatusCreated, messageResponseOf(event))
}

// throttlePress applies one of the press limits, answering with when to retry.
func (s *Server) throttlePress(c *echo.Context, key string, setting settings.Key) error {
	if s.limiter == nil || s.settings == nil {
		return nil
	}
	perMinute := s.settings.Int(c.Request().Context(), setting)
	if perMinute <= 0 {
		return nil
	}

	decision, err := s.limiter.Allow(c.Request().Context(), key,
		cache.RateLimit{Attempts: int(perMinute), Window: time.Minute})
	if err != nil {
		// A limiter that cannot count lets the press through: failing every
		// button in the server because a cache is unwell is the worse outcome.
		s.log.Warn("could not check a press against its limit", "error", err)
		return nil
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}
	return nil
}

type updateControlsRequest struct {
	// Version is the one being replaced. Required: an update that did not say
	// what it was replacing could not be refused as stale.
	Version *int32           `json:"version"`
	Body    string           `json:"body"`
	Actions []actions.Action `json:"actions"`
}

type updateControlsResponse struct {
	messageResponse
	// Version is what a press against the new controls should name.
	Version int32 `json:"version"`
}

func (s *Server) handleUpdateControls(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request updateControlsRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Version == nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			"say which version you are replacing, so a stale update can be refused")
	}

	// A ban or a posting restriction stops an update as it stops a post:
	// replacing a message's text is saying something in the channel.
	if err := s.mayPost(ctx, roomID, user.ID); err != nil {
		return err
	}

	// Filtered like any other text a channel's readers see, so updating is
	// not a way around the operator's list.
	filtered, err := s.applyContentFilter(ctx, request.Body)
	if err != nil {
		return err
	}

	event, controls, err := s.rooms.UpdateMessage(ctx, rooms.Update{
		RoomID:   roomID,
		AuthorID: user.ID,
		Message:  eventIDParam(c),
		Expected: *request.Version,
		Body:     filtered.Body,
		Actions:  request.Actions,
	})
	if err != nil {
		return interactiveError(err)
	}

	if filtered.Flagged {
		s.flagForReview(c, roomID, event.ID, s.rooms.UserAddress(user.Username),
			filtered.Matched, filtered.Body)
	}

	s.publishEvent(ctx, event)
	return c.JSON(http.StatusOK, updateControlsResponse{
		messageResponse: messageResponseOf(event),
		Version:         controls.Version,
	})
}

type privateReplyRequest struct {
	Body string `json:"body"`
}

// privateReply is what the person who pressed receives, and nobody else.
type privateReply struct {
	ID string `json:"id"`
	// Press is the action.pressed event being answered.
	Press string `json:"press"`
	// Message is the message whose control was pressed.
	Message string `json:"message"`
	Sender  string `json:"sender"`
	Body    string `json:"body"`
	// CreatedAt is epoch milliseconds, like every other live frame.
	CreatedAt int64 `json:"createdAt"`
}

type privateReplyResponse struct {
	/*
		Delivered is how many of the person's open connections received it.

		Zero is an honest answer rather than an error: the reply is not kept,
		so somebody not connected when it was sent does not see it, and a
		program should post an ordinary message for anything that must not be
		missed.
	*/
	Delivered int `json:"delivered"`
}

func (s *Server) handlePrivateReply(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request privateReplyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if len(request.Body) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, rooms.ErrEmptyMessage.Error())
	}
	if len(request.Body) > rooms.MaxMessageLength {
		return echo.NewHTTPError(http.StatusBadRequest, rooms.ErrMessageTooLong.Error())
	}

	invocation, err := s.rooms.PrivateReplyTarget(ctx, roomID, user.ID, eventIDParam(c))
	if err != nil {
		return interactiveError(err)
	}

	// Private is not unfiltered: the operator's list applies to what one
	// person is shown exactly as to what a channel is.
	filtered, err := s.applyContentFilter(ctx, request.Body)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(privateReply{
		ID:        uuid.NewString(),
		Press:     invocation.Press.ID,
		Message:   invocation.Message,
		Sender:    s.rooms.UserAddress(user.Username),
		Body:      filtered.Body,
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return err
	}

	frame := realtime.Frame{
		Type:    realtime.FramePrivate,
		Room:    s.rooms.RoomAddress(invocation.Room),
		RoomID:  roomID.String(),
		Payload: payload,
	}

	delivered := 0
	if s.hub != nil {
		delivered = s.hub.SendToUser(invocation.Invoker.ID, frame)
	}
	delivered += s.whisperElsewhere(ctx, invocation.Invoker.ID, frame, delivered)

	return c.JSON(http.StatusCreated, privateReplyResponse{Delivered: delivered})
}

// contextResponse carries what a program remembered.
type contextResponse struct {
	State json.RawMessage `json:"state"`
}

// contextParams reads the room and person a context request names.
func contextParams(c *echo.Context) (uuid.UUID, uuid.UUID, error) {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		return uuid.Nil, uuid.Nil, echo.NewHTTPError(http.StatusNotFound, "nothing remembered")
	}
	return roomID, userID, nil
}

func (s *Server) handleReadContext(c *echo.Context) error {
	caller, _ := currentUser(c)
	roomID, userID, err := contextParams(c)
	if err != nil {
		return err
	}

	state, err := s.rooms.Context(c.Request().Context(), caller.ID, roomID, userID)
	if err != nil {
		return interactiveError(err)
	}
	if state == nil {
		// The same answer whether nothing was ever stored or it was stored by
		// some other program: which is which is not the caller's to learn.
		return echo.NewHTTPError(http.StatusNotFound, "nothing remembered")
	}
	return c.JSON(http.StatusOK, contextResponse{State: state})
}

func (s *Server) handleRememberContext(c *echo.Context) error {
	caller, _ := currentUser(c)
	roomID, userID, err := contextParams(c)
	if err != nil {
		return err
	}

	var request contextResponse
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if len(request.State) == 0 || string(request.State) == "null" {
		return echo.NewHTTPError(http.StatusBadRequest, "say what to remember, or DELETE to forget")
	}

	if err := s.rooms.RememberContext(c.Request().Context(), caller.ID, roomID, userID, request.State); err != nil {
		return interactiveError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleForgetContext(c *echo.Context) error {
	caller, _ := currentUser(c)
	roomID, userID, err := contextParams(c)
	if err != nil {
		return err
	}

	if err := s.rooms.ForgetContext(c.Request().Context(), caller.ID, roomID, userID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

/*
interactiveError turns what answering a press can refuse into responses, and
leaves everything else to the ordinary room errors.

Stale is a conflict like "already answered": the request is well formed and
the state refuses it, and a client needs to tell that apart from something
worth retrying.
*/
func interactiveError(err error) error {
	switch {
	case errors.Is(err, rooms.ErrStaleControls):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, rooms.ErrNotPermitted):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, actions.ErrValueRefused),
		errors.Is(err, actions.ErrInvalidElement),
		errors.Is(err, rooms.ErrContextTooLarge):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, rooms.ErrNotAMessage), errors.Is(err, rooms.ErrNotAPress):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, rooms.ErrInvocationExpired):
		return echo.NewHTTPError(http.StatusGone, err.Error())
	default:
		return roomError(err)
	}
}

/*
privateReachLimit is how long an answer waits for other nodes to say they
delivered it.

Only spent when presence says the person holds connections somewhere else, and
cut short as soon as they have all answered — so the ordinary case is a round
trip through the database. The whole second is spent only when presence counts a
connection that has gone without saying so, which the presence sweep clears.
*/
const privateReachLimit = time.Second

/*
whisperElsewhere sends a private frame to the person's connections on other
nodes, and reports how many received it.

Asked only when presence counts more connections than this node delivered to:
a single node costs nothing, and somebody connected only here costs one
presence read.
*/
func (s *Server) whisperElsewhere(ctx context.Context, userID uuid.UUID, frame realtime.Frame, here int) int {
	if s.broker == nil || s.presence == nil {
		return 0
	}
	// A server running alone has nobody to whisper to, and need not read
	// presence to find that out.
	if _, alone := s.broker.(realtime.Alone); alone {
		return 0
	}
	total, err := s.presence.Connections(ctx, userID)
	if err != nil || total <= here {
		return 0
	}

	elsewhere, err := s.broker.Whisper(ctx, userID, frame, total-here, privateReachLimit)
	if err != nil {
		s.log.Warn("could not share a private answer with the other nodes", "error", err)
	}
	return elsewhere
}
