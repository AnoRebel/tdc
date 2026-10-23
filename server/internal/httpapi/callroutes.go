package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // coturn's REST credential scheme is HMAC-SHA1; it is not used for anything else.
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/push"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

func init() {
	registerRoutes(func(s *Server) {
		if s.rooms == nil {
			return
		}
		if s.realtime != nil {
			s.realtime.SetSignaller(s)
		}

		calls := s.echo.Group("/api/rooms/:id/call", requireAuthentication, s.requireFeature(settings.FeatureCalls))
		calls.GET("", s.handleGetCall, requireChannelPermission(authz.PermChannelRead))
		calls.POST("", s.handleJoinCall, s.requirePosting())
		calls.PATCH("", s.handleCallMedia)
		calls.DELETE("", s.handleLeaveCall)
		calls.POST("/ring", s.handleRing)
		calls.POST("/decline", s.handleDeclineRing)
		calls.POST("/invite", s.handleInviteToCall)
		calls.PUT("/hand", s.handleHand(true))
		calls.DELETE("/hand", s.handleHand(false))
		calls.DELETE("/hand/:userId", s.handleLowerHand)
		calls.POST("/reactions", s.handleCallReaction)
		calls.PUT("/stage", s.handlePresent(true))
		calls.DELETE("/stage", s.handlePresent(false))
		calls.DELETE("/stage/presenter", s.handleClearStage,
			requireChannelPermission(authz.PermModerationRestrict))

		// One call by its identifier, for the people let into it without being
		// in its conversation as well as for its members.
		byID := s.echo.Group("/api/calls/:callId", requireAuthentication, s.requireFeature(settings.FeatureCalls))
		byID.GET("", s.handleCallByID)
		byID.POST("", s.handleJoinCallByID)
		byID.PATCH("", s.handleCallMediaByID)
		byID.DELETE("", s.handleLeaveCallByID)

		s.echo.GET("/api/calls/ice", s.handleICEServers,
			requireAuthentication, s.requireFeature(settings.FeatureCalls))
	})

	documentRoutes(map[string]entry{
		"GET /api/rooms/:id/call":         {Summary: "The call in progress in a conversation", Authentication: "session or token", Permission: "channel.read"},
		"PUT /api/rooms/:id/call/hand":    {Summary: "Raise your hand in the call", Authentication: "session or token"},
		"DELETE /api/rooms/:id/call/hand": {Summary: "Lower your hand", Authentication: "session or token"},
		"DELETE /api/rooms/:id/call/hand/:userId": {
			Summary: "Lower somebody's hand, as whoever presents or a moderator", Authentication: "session or token",
		},
		"POST /api/rooms/:id/call/reactions": {Summary: "Send a passing reaction to everybody in the call", Authentication: "session or token"},
		"PUT /api/rooms/:id/call/stage":      {Summary: "Present your screen in the call, if nobody else is", Authentication: "session or token"},
		"DELETE /api/rooms/:id/call/stage":   {Summary: "Stop presenting your screen", Authentication: "session or token"},
		"DELETE /api/rooms/:id/call/stage/presenter": {
			Summary: "Take whoever is presenting off the stage", Authentication: "session or token", Permission: "moderation.restrict",
		},
		"POST /api/rooms/:id/call/invite":  {Summary: "Invite the people of another call you are in into this one", Authentication: "session or token"},
		"GET /api/calls/:callId":           {Summary: "A call you are in, or are invited into", Authentication: "session or token"},
		"POST /api/calls/:callId":          {Summary: "Join a call you were invited into", Authentication: "session or token", Permission: "call.join"},
		"PATCH /api/calls/:callId":         {Summary: "Say what you are sending in a call", Authentication: "session or token"},
		"DELETE /api/calls/:callId":        {Summary: "Leave a call", Authentication: "session or token"},
		"POST /api/rooms/:id/call":         {Summary: "Join a conversation's call, starting one if none is in progress", Authentication: "session or token", Permission: "call.start or call.join"},
		"PATCH /api/rooms/:id/call":        {Summary: "Say whether you are sending audio and video", Authentication: "session or token"},
		"DELETE /api/rooms/:id/call":       {Summary: "Leave a conversation's call", Authentication: "session or token"},
		"POST /api/rooms/:id/call/ring":    {Summary: "Ring people who may join the call you are in", Authentication: "session or token"},
		"POST /api/rooms/:id/call/decline": {Summary: "Decline being rung into a conversation's call", Authentication: "session or token"},
		"GET /api/calls/ice":               {Summary: "The servers your client uses to connect a call", Authentication: "session or token"},
	})
}

// turnCredentialLifetime is how long minted relay credentials last: longer
// than a call is likely to, and short enough that a leaked pair soon dies.
const turnCredentialLifetime = 4 * time.Hour

// callSweepInterval is how often people with no live connection are taken out
// of their calls.
const callSweepInterval = 15 * time.Second

type callMediaRequest struct {
	Audio bool `json:"audio"`
	Video bool `json:"video"`
	// Held says they are in another call for now, and send nothing to this one.
	Held bool `json:"held"`
}

type callParticipantView struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Address  string `json:"address"`
	Audio    bool   `json:"audio"`
	Video    bool   `json:"video"`
	Held     bool   `json:"held"`
	// Screen says they are presenting their screen.
	Screen bool `json:"screen"`
	// HandRaisedAt is when they raised their hand, while it is up.
	HandRaisedAt string `json:"handRaisedAt,omitempty"`
	JoinedAt     string `json:"joinedAt"`
}

type callView struct {
	ID        string `json:"id"`
	RoomID    string `json:"roomId"`
	StartedAt string `json:"startedAt"`
	// Presenter is who is on the stage, if anybody.
	Presenter    string                `json:"presenter,omitempty"`
	Participants []callParticipantView `json:"participants"`
}

func (s *Server) callViewOf(roomID, callID uuid.UUID, startedAt time.Time, participants []dbgen.ListCallParticipantsRow) callView {
	view := callView{
		ID: callID.String(), RoomID: roomID.String(),
		StartedAt:    startedAt.UTC().Format(time.RFC3339),
		Participants: make([]callParticipantView, 0, len(participants)),
	}
	for _, p := range participants {
		view.Participants = append(view.Participants, callParticipantView{
			UserID: p.UserID.String(), Username: p.Username, Address: s.rooms.UserAddress(p.Username),
			Audio: p.Audio, Video: p.Video, Held: p.Held, Screen: p.Screen, JoinedAt: p.JoinedAt.UTC().Format(time.RFC3339),
		})
	}
	for index, p := range participants {
		if p.HandRaisedAt != nil {
			view.Participants[index].HandRaisedAt = p.HandRaisedAt.UTC().Format(time.RFC3339Nano)
		}
		if p.Screen {
			view.Presenter = p.UserID.String()
		}
	}
	return view
}

func callError(err error) error {
	switch {
	case errors.Is(err, rooms.ErrNoCall):
		return echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	case errors.Is(err, rooms.ErrCallFull):
		return echo.NewHTTPError(http.StatusConflict, "the call is full")
	case errors.Is(err, rooms.ErrNotCallable), errors.Is(err, rooms.ErrHostedElsewhere):
		return echo.NewHTTPError(http.StatusConflict, "calls are not available in this conversation")
	}
	return roomError(err)
}

func (s *Server) handleGetCall(c *echo.Context) error {
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	call, err := s.rooms.OpenCall(c.Request().Context(), roomID, user.ID)
	if err != nil {
		return callError(err)
	}
	return c.JSON(http.StatusOK, s.callViewOf(roomID, call.ID, call.StartedAt, call.Participants))
}

/*
handleJoinCall joins the conversation's call, starting one if there is none.

Posting is already required, with its bans and restrictions, by the route's
guard; on top of it, starting takes call.start and joining call.join, so an
operator can allow one without the other, or messaging without either.
*/
func (s *Server) handleJoinCall(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request callMediaRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	needed := authz.PermCallJoin
	if _, err := s.rooms.OpenCall(ctx, roomID, user.ID); errors.Is(err, rooms.ErrNoCall) {
		needed = authz.PermCallStart
	} else if err != nil {
		return callError(err)
	}
	if decision := currentPermissions(c).AllowsChannel(needed, roomID); !decision.Allowed {
		return echo.NewHTTPError(http.StatusForbidden, decision.Error())
	}

	limit := int(s.settings.Int(ctx, settings.CallsMaxParticipants))
	call, started, err := s.rooms.JoinCall(ctx, roomID, user.ID, request.Audio, request.Video, limit)
	if err != nil {
		return callError(err)
	}
	if started != nil {
		s.publishEvent(ctx, *started)
	}
	// Answered here, so none of their other devices keeps ringing.
	s.endRinging(ctx, user.ID, call.ID)
	view := s.callViewOf(roomID, call.ID, call.StartedAt, call.Participants)
	s.broadcastCallState(ctx, view)
	return c.JSON(http.StatusOK, view)
}

func (s *Server) handleCallMedia(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request callMediaRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	call, err := s.rooms.OpenCall(ctx, roomID, user.ID)
	if err != nil {
		return callError(err)
	}
	if err := s.rooms.SetCallMedia(ctx, call.ID, user.ID, request.Audio, request.Video, request.Held); err != nil {
		return callError(err)
	}
	return s.respondWithCallState(c, roomID, call)
}

func (s *Server) handleLeaveCall(c *echo.Context) error {
	// Leaving finishes even if the caller does not wait for the answer — a
	// tab closed on the way out — so a call is not left open with nobody in it.
	ctx := context.WithoutCancel(c.Request().Context())
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	call, err := s.rooms.OpenCall(ctx, roomID, user.ID)
	if err != nil {
		return callError(err)
	}
	ended, err := s.rooms.LeaveCall(ctx, call.ID, user.ID)
	if err != nil {
		return callError(err)
	}
	if ended != nil {
		s.publishEvent(ctx, *ended)
	}
	if _, err := s.announceCall(ctx, roomID, call); err != nil {
		// Left is left; the others learn it from the sweep or their next read.
		s.log.Warn("could not announce a call somebody left", "error", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) respondWithCallState(c *echo.Context, roomID uuid.UUID, call rooms.Call) error {
	view, err := s.announceCall(c.Request().Context(), roomID, call)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

// announceCall reads a call's participants afresh and tells everybody concerned.
func (s *Server) announceCall(ctx context.Context, roomID uuid.UUID, call rooms.Call) (callView, error) {
	participants, err := s.rooms.CallParticipants(ctx, call.ID)
	if err != nil {
		return callView{}, err
	}
	view := s.callViewOf(roomID, call.ID, call.StartedAt, participants)
	s.broadcastCallState(ctx, view)
	return view, nil
}

/*
broadcastCallState tells a call's participants, and whoever has the
conversation open on this node, who is in the call and what each is sending.

Participants are reached on every node, as private frames are; viewers of the
conversation who are not in the call only on this one, since for them it is a
count in a header and the call's start and end reach every node as events.
*/
func (s *Server) broadcastCallState(ctx context.Context, view callView) {
	if s.hub == nil {
		return
	}
	payload, err := json.Marshal(view)
	if err != nil {
		return
	}
	frame := realtime.Frame{Type: realtime.FrameCallState, RoomID: view.RoomID, Payload: payload}
	roomID, _ := uuid.Parse(view.RoomID)
	s.hub.Publish(ctx, roomID, frame)
	for _, participant := range view.Participants {
		userID, err := uuid.Parse(participant.UserID)
		if err != nil {
			continue
		}
		here := s.hub.SendToUser(userID, frame)
		s.whisperElsewhere(ctx, userID, frame, here)
	}
}

/*
Signal passes one piece of a call's negotiation from one participant to another.

Authorised on every frame, not once at joining: both must be in that open call
now, and both must still be allowed to join calls in its conversation, so a
withdrawn permission or a removal ends somebody's part in the negotiation at
once rather than when they next reconnect.
*/
func (s *Server) Signal(ctx context.Context, from, callID, to uuid.UUID, signal json.RawMessage) error {
	if from == to {
		return errors.New("a participant cannot signal themselves")
	}
	roomID, fromIn := s.rooms.CallRoomOf(ctx, callID, from)
	toRoom, toIn := s.rooms.CallRoomOf(ctx, callID, to)
	if !fromIn || !toIn || roomID != toRoom {
		return rooms.ErrNoCall
	}
	for _, person := range []uuid.UUID{from, to} {
		permissions, err := s.authz.PermissionsFor(ctx, person)
		if err != nil || !permissions.AllowsChannel(authz.PermCallJoin, roomID).Allowed {
			return errors.New("not allowed to take part in this call")
		}
	}

	payload, err := json.Marshal(map[string]any{
		"call": callID.String(), "from": from.String(), "signal": signal,
	})
	if err != nil {
		return err
	}
	frame := realtime.Frame{Type: realtime.FrameCallSignal, RoomID: roomID.String(), Payload: payload}
	here := s.hub.SendToUser(to, frame)
	s.whisperElsewhere(ctx, to, frame, here)
	return nil
}

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

/*
handleICEServers gives a client the servers it connects calls through.

Relay credentials are minted per person and expire, in the scheme coturn's
use-auth-secret understands: the username is when they expire and who they are
for, the password an HMAC of it under the secret the relay shares with this
server. Without a secret no relay is offered, even if one is listed, because a
relay this server cannot mint credentials for is one it cannot let anybody use.
*/
func (s *Server) handleICEServers(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	servers := []iceServer{}
	if stun := settings.ListOf(s.settings.Get(ctx, settings.CallsSTUNURLs)); len(stun) > 0 {
		servers = append(servers, iceServer{URLs: stun})
	}
	turn := settings.ListOf(s.settings.Get(ctx, settings.CallsTURNURLs))
	if len(turn) > 0 && s.liveConfig().turnSecret != "" {
		username, credential := turnCredential(s.liveConfig().turnSecret, user.ID, time.Now().Add(turnCredentialLifetime))
		servers = append(servers, iceServer{URLs: turn, Username: username, Credential: credential})
	}
	return c.JSON(http.StatusOK, map[string]any{"iceServers": servers, "relay": len(turn) > 0 && s.liveConfig().turnSecret != ""})
}

// turnCredential mints a relay username and password valid until expiry.
func turnCredential(secret string, userID uuid.UUID, expiry time.Time) (string, string) {
	username := fmt.Sprintf("%d:%s", expiry.Unix(), userID)
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	return username, base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// sweepCallsPeriodically takes people with no live connection out of calls.
func (s *Server) sweepCallsPeriodically(ctx context.Context) {
	ticker := time.NewTicker(callSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SweepCalls(ctx)
		}
	}
}

// SweepCalls makes one sweep. Exported so a test can make one when it chooses.
func (s *Server) SweepCalls(ctx context.Context) {
	if s.settings == nil || !s.settings.Enabled(ctx, settings.FeatureCalls) {
		return
	}
	changed, ended, err := s.rooms.SweepCalls(ctx)
	if err != nil {
		s.log.Warn("could not sweep calls", "error", err)
	}
	for _, event := range ended {
		s.publishEvent(ctx, event)
	}
	for _, swept := range changed {
		if _, err := s.announceCall(ctx, swept.RoomID, rooms.Call{Call: dbgen.Call{ID: swept.CallID}}); err != nil {
			s.log.Warn("could not announce a swept call", "error", err)
		}
	}
}

// ringTimeout is how long a ring lasts before it becomes a missed call.
var ringTimeout = 45 * time.Second

// ringsPerCaller bounds how often one person can ring, so ringing is not a way
// to pester somebody.
var ringsPerCaller = cache.RateLimit{Attempts: 10, Window: 5 * time.Minute}

// maxDeclineMessage is how long the words sent with a decline may be.
const maxDeclineMessage = 280

type ringRequest struct {
	// Users to ring, by id. Empty rings everybody else in a direct
	// conversation, and nobody in a channel.
	Users []string `json:"users"`
}

type ringFrame struct {
	Call     string `json:"call"`
	RoomID   string `json:"roomId"`
	RoomName string `json:"roomName"`
	Kind     string `json:"kind"`
	// Conference marks an invitation into a call merged with another; the
	// person invited is not in its conversation and is told nothing of it.
	Conference bool `json:"conference,omitempty"`
	From       struct {
		UserID   string `json:"userId"`
		Username string `json:"username"`
		Address  string `json:"address"`
	} `json:"from"`
}

/*
handleRing rings people into the call the caller is in.

Each person rung must be somebody who could join — in the conversation and
allowed to call — and must accept calls from the caller under their own
privacy setting. Who was rung is returned; who was not is returned as a count
only, so ringing cannot be used to learn anybody's settings or blocks.
*/
func (s *Server) handleRing(c *echo.Context) error {
	ctx := c.Request().Context()
	caller, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request ringRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	call, err := s.rooms.OpenCall(ctx, roomID, caller.ID)
	if err != nil {
		return callError(err)
	}
	if _, in := s.rooms.CallRoomOf(ctx, call.ID, caller.ID); !in {
		return echo.NewHTTPError(http.StatusConflict, "join the call before ringing anybody into it")
	}
	if s.limiter != nil {
		decision, err := s.limiter.Allow(ctx, "call.ring:"+caller.ID.String(), ringsPerCaller)
		if err == nil && !decision.Allowed {
			return echo.NewHTTPError(http.StatusTooManyRequests, "you have rung a lot of people just now; try again shortly")
		}
	}

	room, err := s.rooms.ByID(ctx, roomID)
	if err != nil {
		return roomError(err)
	}
	targets := []uuid.UUID{}
	if len(request.Users) == 0 && room.Kind == "direct" {
		members, err := s.rooms.MemberIDs(ctx, roomID)
		if err != nil {
			return err
		}
		for _, member := range members {
			if member != caller.ID {
				targets = append(targets, member)
			}
		}
	}
	for _, raw := range request.Users {
		if id, err := uuid.Parse(raw); err == nil && id != caller.ID {
			targets = append(targets, id)
		}
	}

	present := map[uuid.UUID]bool{}
	for _, participant := range call.Participants {
		present[participant.UserID] = true
	}

	frame := ringFrame{Call: call.ID.String(), RoomID: roomID.String(), RoomName: room.Name, Kind: room.Kind}
	frame.From.UserID = caller.ID.String()
	frame.From.Username = caller.Username
	frame.From.Address = s.rooms.UserAddress(caller.Username)
	payload, _ := json.Marshal(frame)

	rung := []string{}
	for _, target := range targets {
		if present[target] || !s.mayRing(ctx, caller.ID, target, roomID) {
			continue
		}
		ring := realtime.Frame{Type: realtime.FrameCallRing, RoomID: roomID.String(), Payload: payload}
		here := s.hub.SendToUser(target, ring)
		s.whisperElsewhere(ctx, target, ring, here)
		rung = append(rung, target.String())
		s.missedIfUnanswered(target, caller, call)
		s.pushRing(target, caller.Username, room, call.ID)
	}
	return c.JSON(http.StatusOK, map[string]any{"rung": rung, "notRung": len(targets) - len(rung)})
}

/*
pushRing reaches the devices of somebody rung whose browser is in the
background or closed, where the live frame cannot. A device showing the
application already rings from the frame, and its worker shows nothing for the
push then.
*/
func (s *Server) pushRing(target uuid.UUID, from string, room dbgen.Room, call uuid.UUID) {
	if s.push == nil || !s.push.Enabled() {
		return
	}
	where := "A direct call"
	if room.Kind != "direct" && room.Name != "" {
		where = "In #" + room.Name
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.push.Send(ctx, target, push.Payload{
			Title: from + " is calling", Body: where, Room: room.ID.String(), Call: call.String(),
		})
	}()
}

// mayRing is whether somebody may be rung into a conversation's call by another.
func (s *Server) mayRing(ctx context.Context, caller, target, roomID uuid.UUID) bool {
	if _, err := s.rooms.RequireMembership(ctx, roomID, target); err != nil {
		return false
	}
	permissions, err := s.authz.PermissionsFor(ctx, target)
	if err != nil || !permissions.AllowsChannel(authz.PermCallJoin, roomID).Allowed {
		return false
	}
	return s.moderation == nil || s.moderation.MayCall(ctx, caller, target)
}

/*
missedIfUnanswered stops the ring after a while and, if the person did not
join, tells them they missed a call. On the node that rang, which is enough:
the notification is stored, and the frame reaches their devices anywhere.
*/
func (s *Server) missedIfUnanswered(target uuid.UUID, caller dbgen.User, call rooms.Call) {
	time.AfterFunc(ringTimeout, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, in := s.rooms.CallRoomOf(ctx, call.ID, target); in {
			return
		}
		s.endRinging(ctx, target, call.ID)
		record, err := s.rooms.CallRecord(ctx, call.ID)
		if err != nil || record.StartedEvent == nil || s.notify == nil {
			return
		}
		s.notify.About(ctx, target, caller.ID, s.rooms.UserAddress(caller.Username),
			record.RoomID, *record.StartedEvent, notify.ReasonMissedCall, "")
	})
}

// endRinging tells all of somebody's devices to stop ringing for a call.
func (s *Server) endRinging(ctx context.Context, userID, callID uuid.UUID) {
	if s.hub == nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{"call": callID.String()})
	frame := realtime.Frame{Type: realtime.FrameCallRingEnded, Payload: payload}
	here := s.hub.SendToUser(userID, frame)
	s.whisperElsewhere(ctx, userID, frame, here)
}

// handleDeclineRing stops somebody's devices ringing and tells the call.
func (s *Server) handleDeclineRing(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request struct {
		// Message is a few words for the caller, shown with the decline. The
		// client also posts it in the conversation, where it stays.
		Message string `json:"message"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	message := strings.TrimSpace(request.Message)
	if utf8.RuneCountInString(message) > maxDeclineMessage {
		return echo.NewHTTPError(http.StatusBadRequest, "a message with a decline is a few words, not a letter")
	}
	call, err := s.rooms.OpenCall(ctx, roomID, user.ID)
	if err != nil {
		return callError(err)
	}
	s.endRinging(ctx, user.ID, call.ID)

	payload, _ := json.Marshal(map[string]string{
		"call": call.ID.String(), "userId": user.ID.String(), "username": user.Username,
		"message": message,
	})
	frame := realtime.Frame{Type: realtime.FrameCallDeclined, RoomID: roomID.String(), Payload: payload}
	for _, participant := range call.Participants {
		here := s.hub.SendToUser(participant.UserID, frame)
		s.whisperElsewhere(ctx, participant.UserID, frame, here)
	}
	return c.NoContent(http.StatusNoContent)
}

/*
handleInviteToCall merges the people of another call into this one.

The caller must be in this call, and each person invited must be in another
call with the caller now; anybody else is skipped and only counted, as with
ringing. Each invitee is rung with a conference invitation that names who is
inviting and nothing of the conversation, which stays closed to them.
*/
func (s *Server) handleInviteToCall(c *echo.Context) error {
	ctx := c.Request().Context()
	caller, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request ringRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	call, err := s.rooms.OpenCall(ctx, roomID, caller.ID)
	if err != nil {
		return callError(err)
	}
	if s.limiter != nil {
		decision, err := s.limiter.Allow(ctx, "call.ring:"+caller.ID.String(), ringsPerCaller)
		if err == nil && !decision.Allowed {
			return echo.NewHTTPError(http.StatusTooManyRequests, "you have rung a lot of people just now; try again shortly")
		}
	}

	var wanted []uuid.UUID
	for _, raw := range request.Users {
		id, err := uuid.Parse(raw)
		if err != nil || id == caller.ID {
			continue
		}
		if s.moderation != nil && !s.moderation.MayCall(ctx, caller.ID, id) {
			continue
		}
		wanted = append(wanted, id)
	}
	invited, err := s.rooms.InviteToCall(ctx, call.ID, caller.ID, wanted)
	if err != nil {
		return callError(err)
	}

	frame := ringFrame{Call: call.ID.String(), RoomID: roomID.String(), Kind: "conference", Conference: true}
	frame.From.UserID = caller.ID.String()
	frame.From.Username = caller.Username
	frame.From.Address = s.rooms.UserAddress(caller.Username)
	payload, _ := json.Marshal(frame)
	names := []string{}
	for _, target := range invited {
		ring := realtime.Frame{Type: realtime.FrameCallRing, Payload: payload}
		here := s.hub.SendToUser(target, ring)
		s.whisperElsewhere(ctx, target, ring, here)
		names = append(names, target.String())
	}
	return c.JSON(http.StatusOK, map[string]any{"invited": names, "notInvited": len(request.Users) - len(names)})
}

// callByID reads the call a route names, for somebody entitled to it.
func (s *Server) callByID(c *echo.Context) (rooms.Call, error) {
	user, _ := currentUser(c)
	callID, err := uuid.Parse(c.Param("callId"))
	if err != nil {
		return rooms.Call{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	call, _, err := s.rooms.CallFor(c.Request().Context(), callID, user.ID)
	if err != nil {
		return rooms.Call{}, callError(err)
	}
	return call, nil
}

func (s *Server) handleCallByID(c *echo.Context) error {
	call, err := s.callByID(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s.callViewOf(call.RoomID, call.ID, call.StartedAt, call.Participants))
}

func (s *Server) handleJoinCallByID(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	call, err := s.callByID(c)
	if err != nil {
		return err
	}
	if decision := currentPermissions(c).AllowsChannel(authz.PermCallJoin, call.RoomID); !decision.Allowed {
		return echo.NewHTTPError(http.StatusForbidden, decision.Error())
	}
	var request callMediaRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	limit := int(s.settings.Int(ctx, settings.CallsMaxParticipants))
	joined, err := s.rooms.JoinInvited(ctx, call.ID, user.ID, request.Audio, request.Video, limit)
	if err != nil {
		return callError(err)
	}
	s.endRinging(ctx, user.ID, call.ID)
	view := s.callViewOf(joined.RoomID, joined.ID, joined.StartedAt, joined.Participants)
	s.broadcastCallState(ctx, view)
	return c.JSON(http.StatusOK, view)
}

func (s *Server) handleCallMediaByID(c *echo.Context) error {
	user, _ := currentUser(c)
	call, err := s.callByID(c)
	if err != nil {
		return err
	}
	var request callMediaRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if err := s.rooms.SetCallMedia(c.Request().Context(), call.ID, user.ID, request.Audio, request.Video, request.Held); err != nil {
		return callError(err)
	}
	return s.respondWithCallState(c, call.RoomID, call)
}

func (s *Server) handleLeaveCallByID(c *echo.Context) error {
	ctx := context.WithoutCancel(c.Request().Context())
	user, _ := currentUser(c)
	call, err := s.callByID(c)
	if err != nil {
		return err
	}
	ended, err := s.rooms.LeaveCall(ctx, call.ID, user.ID)
	if err != nil {
		return callError(err)
	}
	if ended != nil {
		s.publishEvent(ctx, *ended)
	}
	if _, err := s.announceCall(ctx, call.RoomID, call); err != nil {
		s.log.Warn("could not announce a call somebody left", "error", err)
	}
	return c.NoContent(http.StatusNoContent)
}

/*
handlePresent puts the caller's screen on the stage, or takes it off. The
stage holds one presenter; a second is refused and told who has it, by the
call's own state, which everybody already sees.
*/
func (s *Server) handlePresent(presenting bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		user, _ := currentUser(c)
		roomID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "no such room")
		}
		call, err := s.callForPresenting(c, roomID)
		if err != nil {
			return err
		}
		if err := s.rooms.Present(ctx, call.ID, user.ID, presenting); err != nil {
			if errors.Is(err, rooms.ErrStageTaken) {
				return echo.NewHTTPError(http.StatusConflict, "somebody else is presenting; ask them to stop first")
			}
			return callError(err)
		}
		return s.respondWithCallState(c, call.RoomID, call)
	}
}

// callForPresenting finds the call in a room for somebody in it, whether they
// belong to the conversation or were let into the call alone.
func (s *Server) callForPresenting(c *echo.Context, roomID uuid.UUID) (rooms.Call, error) {
	user, _ := currentUser(c)
	ctx := c.Request().Context()
	call, err := s.rooms.OpenCallIn(ctx, roomID)
	if err != nil {
		return rooms.Call{}, callError(err)
	}
	if _, in := s.rooms.CallRoomOf(ctx, call.ID, user.ID); !in {
		return rooms.Call{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	return call, nil
}

func (s *Server) handleClearStage(c *echo.Context) error {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	call, err := s.rooms.OpenCallIn(c.Request().Context(), roomID)
	if err != nil {
		return callError(err)
	}
	if err := s.rooms.ClearStage(c.Request().Context(), call.ID); err != nil {
		return callError(err)
	}
	return s.respondWithCallState(c, roomID, call)
}

// The reactions somebody can send in a call. A fixed set, so a reaction is a
// gesture rather than a message, and nobody can paint the call with text.
var callReactions = map[string]bool{
	"clap": true, "laugh": true, "heart": true, "thumbs_up": true, "surprise": true, "celebrate": true,
}

// Reactions one person may send, so a held key cannot flood a call.
var reactionsPerPerson = cache.RateLimit{Attempts: 20, Window: 10 * time.Second}

func (s *Server) handleHand(raised bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		user, _ := currentUser(c)
		roomID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "no such room")
		}
		call, err := s.callForPresenting(c, roomID)
		if err != nil {
			return err
		}
		if err := s.rooms.SetHand(c.Request().Context(), call.ID, user.ID, raised); err != nil {
			return callError(err)
		}
		return s.respondWithCallState(c, call.RoomID, call)
	}
}

// handleLowerHand lowers somebody else's hand: for whoever is presenting, who
// is taking the questions, or for a moderator of the conversation.
func (s *Server) handleLowerHand(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	target, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "nobody in the call by that name")
	}
	call, err := s.callForPresenting(c, roomID)
	if err != nil {
		return err
	}
	presenting := s.rooms.Presenter(ctx, call.ID)
	mayModerate := currentPermissions(c).AllowsChannel(authz.PermModerationRestrict, roomID).Allowed
	if (presenting == nil || *presenting != user.ID) && !mayModerate {
		return echo.NewHTTPError(http.StatusForbidden, "only whoever is presenting or a moderator lowers somebody else's hand")
	}
	if err := s.rooms.SetHand(ctx, call.ID, target, false); err != nil {
		return callError(err)
	}
	return s.respondWithCallState(c, call.RoomID, call)
}

type callReactionRequest struct {
	// Kind is one of clap, laugh, heart, thumbs_up, surprise, celebrate.
	Kind string `json:"kind"`
}

// handleCallReaction passes a reaction to everybody in the call, and no further.
func (s *Server) handleCallReaction(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request callReactionRequest
	if err := c.Bind(&request); err != nil || !callReactions[request.Kind] {
		return echo.NewHTTPError(http.StatusBadRequest, "a reaction is one of clap, laugh, heart, thumbs_up, surprise or celebrate")
	}
	call, err := s.callForPresenting(c, roomID)
	if err != nil {
		return err
	}
	if s.limiter != nil {
		decision, err := s.limiter.Allow(ctx, "call.reaction:"+user.ID.String(), reactionsPerPerson)
		if err == nil && !decision.Allowed {
			return echo.NewHTTPError(http.StatusTooManyRequests, "that is a lot of reactions; give it a moment")
		}
	}
	payload, _ := json.Marshal(map[string]string{
		"call": call.ID.String(), "roomId": call.RoomID.String(),
		"from": user.ID.String(), "username": user.Username, "kind": request.Kind,
	})
	frame := realtime.Frame{Type: realtime.FrameCallReaction, Payload: payload}
	if s.hub != nil {
		for _, participant := range call.Participants {
			here := s.hub.SendToUser(participant.UserID, frame)
			s.whisperElsewhere(ctx, participant.UserID, frame, here)
		}
	}
	return c.NoContent(http.StatusNoContent)
}
