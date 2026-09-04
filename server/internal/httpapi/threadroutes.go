package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type replyRequest struct {
	Body        string `json:"body"`
	ClientToken string `json:"clientToken"`
}

// threadResponse is a thread's root and its replies.
type threadResponse struct {
	Root    messageResponse   `json:"root"`
	Replies []messageResponse `json:"replies"`
}

func (s *Server) handleThread(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	root, replies, err := s.rooms.Thread(c.Request().Context(), roomID, user.ID, c.Param("eventId"))
	if err != nil {
		return roomError(err)
	}

	response := threadResponse{
		Root:    messageResponseOf(root),
		Replies: make([]messageResponse, 0, len(replies)),
	}
	for _, reply := range replies {
		response.Replies = append(response.Replies, messageResponseOf(reply))
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleReplyInThread(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request replyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	reply, err := s.rooms.ReplyInThread(ctx, roomID, user.ID,
		c.Param("eventId"), request.Body, request.ClientToken)
	if err != nil {
		return roomError(err)
	}

	// Sending clears any typing indication, so it does not linger after the
	// message it announced has arrived.
	if err := s.presence.StopTyping(ctx, roomID, user.ID); err != nil {
		s.log.Error("cannot withdraw typing indication", "error", err)
	}

	s.publishEvent(ctx, reply)
	return c.JSON(http.StatusCreated, messageResponseOf(reply))
}

type openDirectRequest struct {
	// UserIDs of the other participants. The caller is included
	// automatically, so a client cannot open a conversation it is not in.
	UserIDs []string `json:"userIds"`
}

func (s *Server) handleOpenDirect(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request openDirectRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	participants := []uuid.UUID{user.ID}
	for _, raw := range request.UserIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "not a valid user identifier: "+raw)
		}
		// Confirm the person exists before opening a conversation with them,
		// so a typo does not create a conversation with nobody.
		if _, err := s.identity.UserByID(ctx, id); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "no such person")
		}
		participants = append(participants, id)
	}

	if len(participants) < 2 {
		return echo.NewHTTPError(http.StatusBadRequest,
			"a direct conversation needs at least one other person")
	}

	room, err := s.rooms.OpenDirect(ctx, participants)
	if err != nil {
		return roomError(err)
	}

	return c.JSON(http.StatusOK, s.roomResponse(room, true))
}

func (s *Server) handleListDirect(c *echo.Context) error {
	user, _ := currentUser(c)

	direct, err := s.rooms.DirectRooms(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make([]roomResponse, 0, len(direct))
	for _, room := range direct {
		response = append(response, s.roomResponse(room, true))
	}
	return c.JSON(http.StatusOK, response)
}

type markReadRequest struct {
	Sequence int64 `json:"sequence"`
}

func (s *Server) handleMarkRead(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request markReadRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}
	if err := s.rooms.MarkRead(ctx, roomID, user.ID, request.Sequence); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleUnreadCounts(c *echo.Context) error {
	user, _ := currentUser(c)

	counts, err := s.rooms.UnreadCounts(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make(map[string]int64, len(counts))
	for roomID, count := range counts {
		response[roomID.String()] = count
	}
	return c.JSON(http.StatusOK, response)
}

// presenceResponse is what one person's presence looks like to another.
type presenceResponse struct {
	UserID     string    `json:"userId"`
	State      string    `json:"state"`
	Status     string    `json:"status"`
	StatusText string    `json:"statusText,omitempty"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

func (s *Server) handleRoomPresence(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// Membership is checked first: who is present in a room is not something
	// an outsider may learn.
	members, err := s.rooms.Members(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.ID)
	}

	states, err := s.presence.ForMany(ctx, ids)
	if err != nil {
		return err
	}

	response := make([]presenceResponse, 0, len(members))
	for _, member := range members {
		state := states[member.ID]
		response = append(response, presenceResponse{
			UserID:     member.ID.String(),
			State:      string(state.State),
			Status:     state.Status,
			StatusText: state.StatusText,
			LastSeenAt: state.LastSeenAt,
		})
	}

	return c.JSON(http.StatusOK, response)
}

type setStatusRequest struct {
	Status string `json:"status"`
	Text   string `json:"text"`
	// ExpiresInSeconds, when set, makes the status lapse on its own.
	ExpiresInSeconds int64 `json:"expiresInSeconds"`
	Invisible        *bool `json:"invisible"`
}

func (s *Server) handleSetStatus(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request setStatusRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if request.Status != "" {
		var expiresAt *time.Time
		if request.ExpiresInSeconds > 0 {
			at := time.Now().Add(time.Duration(request.ExpiresInSeconds) * time.Second)
			expiresAt = &at
		}
		if err := s.presence.SetStatus(ctx, user.ID, request.Status, request.Text, expiresAt); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	if request.Invisible != nil {
		if err := s.presence.SetInvisible(ctx, user.ID, *request.Invisible); err != nil {
			return err
		}
	}

	state, err := s.presence.Of(ctx, user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, presenceResponse{
		UserID:     user.ID.String(),
		State:      string(state.State),
		Status:     state.Status,
		StatusText: state.StatusText,
		LastSeenAt: state.LastSeenAt,
	})
}

func (s *Server) handleListStatuses(c *echo.Context) error {
	statuses, err := s.presence.Statuses(c.Request().Context())
	if err != nil {
		return err
	}

	response := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		response = append(response, map[string]any{
			"key":                     status.Key,
			"label":                   status.Label,
			"suppressesNotifications": status.SuppressesNotifications,
		})
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleStartTyping(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}
	if err := s.presence.StartTyping(ctx, roomID, user.ID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleListTyping(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	typing, err := s.presence.Typing(ctx, roomID, user.ID)
	if err != nil {
		return err
	}

	response := make([]map[string]string, 0, len(typing))
	for _, row := range typing {
		response = append(response, map[string]string{
			"userId":      row.UserID.String(),
			"displayName": row.DisplayName,
		})
	}
	return c.JSON(http.StatusOK, response)
}

// threadSummary is how many replies one message has.
type threadSummary struct {
	ReplyCount  int64 `json:"replyCount"`
	LastReplyAt int64 `json:"lastReplyAt"`
}

/*
handleThreadSummaries returns reply counts for a room's timeline in one call.

A count per message is what a timeline needs to render, and asking for each one
separately meant a room with fifty messages made fifty requests to draw itself.
The batch query already existed; only the way in was missing.

Scoped to the room and its membership, so this cannot be used to count replies
in a conversation the caller cannot read.
*/
func (s *Server) handleThreadSummaries(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// The same check the single-thread endpoint makes. Counts disclose that a
	// conversation is active, so they are not more public than its messages.
	if _, err := s.rooms.Visible(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	summaries, err := s.rooms.RoomThreadSummaries(ctx, roomID)
	if err != nil {
		return err
	}

	response := make(map[string]threadSummary, len(summaries))
	for rootID, summary := range summaries {
		response[rootID] = threadSummary{
			ReplyCount:  summary.ReplyCount,
			LastReplyAt: summary.LastReplyAt,
		}
	}

	return c.JSON(http.StatusOK, response)
}
