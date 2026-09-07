package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

type forwardRequest struct {
	// To is where the message is being forwarded, as a room identifier.
	To string `json:"to"`
	// Comment is what the forwarder wants to say alongside it.
	Comment string `json:"comment,omitempty"`
	// ClientToken makes a retried forward produce one message rather than two.
	ClientToken string `json:"clientToken,omitempty"`
}

/*
handleForward sends a copy of a message to another conversation.

A copy rather than a link. A link would keep the forwarded message tied to the
original — changing when it is edited, disappearing when it is deleted, and
readable only by people who can see where it came from, which would either leak
a private room or show an empty box.
*/
func (s *Server) handleForward(c *echo.Context) error {
	user, _ := currentUser(c)

	fromRoomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request forwardRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	toRoomID, err := uuid.Parse(request.To)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a room to forward to")
	}

	event, err := s.rooms.Forward(
		c.Request().Context(), fromRoomID, toRoomID, user.ID,
		eventIDParam(c), request.Comment, request.ClientToken)
	if err != nil {
		return roomError(err)
	}

	ctx := c.Request().Context()

	// Whoever wrote it hears that it has been repeated elsewhere. Where it
	// went is deliberately not said: the destination may be a room they cannot
	// see, and naming it would disclose that the room exists.
	if s.notify != nil {
		if original, err := s.rooms.EventInRoom(ctx, fromRoomID, eventIDParam(c)); err == nil &&
			original.SenderID != nil {
			s.notify.About(ctx, *original.SenderID, user.ID,
				s.rooms.UserAddress(user.Username), fromRoomID, original.ID,
				notify.ReasonForward, "")
		}
	}

	s.publishEvent(ctx, event)

	return c.JSON(http.StatusCreated, messageResponseOf(event))
}

type scheduleRequest struct {
	Body        string   `json:"body"`
	Attachments []string `json:"attachments,omitempty"`
	// ThreadRoot keeps a scheduled reply a reply.
	ThreadRoot string `json:"threadRoot,omitempty"`
	// SendAt is when to send it, as RFC 3339. An absolute instant rather than
	// a delay, so a request that is slow to arrive still sends when meant.
	SendAt string `json:"sendAt"`
}

type scheduledResponse struct {
	ID          string   `json:"id"`
	RoomID      string   `json:"roomId"`
	Body        string   `json:"body"`
	Attachments []string `json:"attachments,omitempty"`
	ThreadRoot  string   `json:"threadRoot,omitempty"`
	SendAt      int64    `json:"sendAt"`
}

func (s *Server) handleScheduleMessage(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request scheduleRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	sendAt, err := time.Parse(time.RFC3339, request.SendAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			"when to send it, as a date and time")
	}

	attachmentIDs := make([]uuid.UUID, 0, len(request.Attachments))
	for _, raw := range request.Attachments {
		id, err := uuid.Parse(raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "that is not an attachment")
		}
		attachmentIDs = append(attachmentIDs, id)
	}

	/*
		Filtered when it is scheduled rather than when it is sent.

		Blocking has to refuse while somebody is here to be told: a message
		accepted now and silently dropped in an hour is a message its author
		believes was sent. Masking applies now for the same reason — what they
		scheduled is what they can see they scheduled.

		The cost is that a word added to the list afterwards does not catch
		what is already queued. That is the right trade: the alternative
		refuses a message with nobody to tell.
	*/
	filtered, err := s.applyContentFilter(c.Request().Context(), request.Body)
	if err != nil {
		return err
	}

	scheduled, err := s.rooms.Schedule(
		c.Request().Context(), roomID, user.ID,
		filtered.Body, attachmentIDs, request.ThreadRoot, sendAt)

	switch {
	case errors.Is(err, rooms.ErrScheduledInThePast):
		return echo.NewHTTPError(http.StatusBadRequest, "that time has already passed")
	case errors.Is(err, rooms.ErrScheduledTooFar):
		return echo.NewHTTPError(http.StatusBadRequest, "that is too far ahead to schedule")
	case err != nil:
		return roomError(err)
	}

	return c.JSON(http.StatusCreated, scheduledResponseOf(scheduled))
}

// handleListScheduled returns somebody's own outstanding messages.
func (s *Server) handleListScheduled(c *echo.Context) error {
	user, _ := currentUser(c)

	scheduled, err := s.rooms.ScheduledFor(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make([]scheduledResponse, 0, len(scheduled))
	for _, item := range scheduled {
		response = append(response, scheduledResponseOf(item))
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleCancelScheduled(c *echo.Context) error {
	user, _ := currentUser(c)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such scheduled message")
	}

	err = s.rooms.CancelScheduled(c.Request().Context(), id, user.ID)
	if errors.Is(err, rooms.ErrNoSuchScheduledMessage) {
		return echo.NewHTTPError(http.StatusNotFound, "no such scheduled message")
	}
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

func scheduledResponseOf(scheduled rooms.ScheduledMessage) scheduledResponse {
	response := scheduledResponse{
		ID:         scheduled.ID.String(),
		RoomID:     scheduled.RoomID.String(),
		Body:       scheduled.Body,
		ThreadRoot: scheduled.ThreadRootID,
		SendAt:     scheduled.SendAt,
	}
	for _, id := range scheduled.AttachmentIDs {
		response.Attachments = append(response.Attachments, id.String())
	}
	return response
}
