package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// roomResponse is the public shape of a room.
type roomResponse struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Topic      string `json:"topic"`
	Kind       string `json:"kind"`
	Visibility string `json:"visibility"`
	// Address is the federated form, room@server.
	Address  string `json:"address"`
	Archived bool   `json:"archived"`
	// Member says whether the requester is in this room, so a client can
	// distinguish a room it can read from one it must join first.
	Member bool `json:"member"`
}

func (s *Server) roomResponse(room dbgen.Room, member bool) roomResponse {
	slug := ""
	if room.Slug != nil {
		slug = *room.Slug
	}

	return roomResponse{
		ID:         room.ID.String(),
		Slug:       slug,
		Name:       room.Name,
		Topic:      room.Topic,
		Kind:       room.Kind,
		Visibility: room.Visibility,
		Address:    s.rooms.RoomAddress(room),
		Archived:   room.ArchivedAt != nil,
		Member:     member,
	}
}

// messageResponse is the public shape of an event in a timeline.
type messageResponse struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Sender   string          `json:"sender"`
	Sequence int64           `json:"sequence"`
	Content  json.RawMessage `json:"content"`
	// Attachments is absent when a message carries no files, which is most of
	// them.
	Attachments []attachmentResponse `json:"attachments,omitempty"`
	SentAt      time.Time            `json:"sentAt"`
	Redacted    bool                 `json:"redacted"`
}

func messageResponseOf(event dbgen.Event) messageResponse {
	return messageResponse{
		ID:       event.ID,
		Type:     event.Type,
		Sender:   event.SenderAddress,
		Sequence: event.Sequence,
		Content:  json.RawMessage(event.Content),
		SentAt:   time.UnixMilli(event.CreatedAt),
		Redacted: event.RedactedAt != nil,
	}
}

// withAttachments fills in a message's attachments.
//
// Looked up per message rather than joined into the timeline query: most
// messages carry none, and the common case should not pay for the rare one.
func (s *Server) withAttachments(ctx context.Context, message messageResponse) messageResponse {
	files, err := s.attachments.ForEvent(ctx, message.ID)
	if err != nil || len(files) == 0 {
		// A message whose attachments cannot be read is still a message. The
		// text is what matters most, and hiding it because a file is missing
		// would be the worse failure.
		return message
	}

	message.Attachments = make([]attachmentResponse, 0, len(files))
	for _, file := range files {
		message.Attachments = append(message.Attachments, attachmentResponseOf(file))
	}
	return message
}

type createRoomRequest struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Topic      string `json:"topic"`
	Visibility string `json:"visibility"`
}

func (s *Server) handleListRooms(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	discoverable, err := s.rooms.Discoverable(ctx, user.ID)
	if err != nil {
		return err
	}

	// Which of these the requester is actually in, so the client can show the
	// difference between a room they read and one they may join.
	joined, err := s.rooms.RoomsUserIsIn(ctx, user.ID)
	if err != nil {
		return err
	}
	member := make(map[uuid.UUID]bool, len(joined))
	for _, room := range joined {
		member[room.ID] = true
	}

	response := make([]roomResponse, 0, len(discoverable))
	for _, room := range discoverable {
		response = append(response, s.roomResponse(room, member[room.ID]))
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleCreateRoom(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request createRoomRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	room, err := s.rooms.Create(ctx, rooms.NewRoom{
		Slug:       request.Slug,
		Name:       request.Name,
		Topic:      request.Topic,
		Visibility: request.Visibility,
		CreatedBy:  user.ID,
	})
	if err != nil {
		return roomError(err)
	}

	return c.JSON(http.StatusCreated, s.roomResponse(room, true))
}

func (s *Server) handleGetRoom(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	room, err := s.rooms.Visible(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	_, membershipErr := s.rooms.RequireMembership(ctx, roomID, user.ID)
	return c.JSON(http.StatusOK, s.roomResponse(room, membershipErr == nil))
}

func (s *Server) handleJoinRoom(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if err := s.rooms.Join(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	room, err := s.rooms.Visible(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}
	return c.JSON(http.StatusOK, s.roomResponse(room, true))
}

func (s *Server) handleLeaveRoom(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if err := s.rooms.Leave(c.Request().Context(), roomID, user.ID); err != nil {
		return roomError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleRoomTimeline(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	timeline, err := s.rooms.Timeline(ctx, roomID, user.ID, 100)
	if err != nil {
		return roomError(err)
	}

	response := make([]messageResponse, 0, len(timeline))
	for _, event := range timeline {
		response = append(response, s.withAttachments(ctx, messageResponseOf(event)))
	}
	return c.JSON(http.StatusOK, response)
}

type sendMessageRequest struct {
	// AttachmentIDs are uploads to bind to this message. They were uploaded
	// beforehand, so a refusal here costs only the binding, not the transfer.
	AttachmentIDs []string `json:"attachmentIds,omitempty"`
	Body          string   `json:"body"`
	// ClientToken makes a retry safe: resending with the same token returns
	// the original message rather than posting twice.
	ClientToken string `json:"clientToken"`
}

func (s *Server) handleSendMessage(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request sendMessageRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	// Checked before the message is accepted, so a refusal does not leave a
	// message posted with its files missing.
	attachmentIDs, err := parseAttachmentIDs(request.AttachmentIDs)
	if err != nil {
		return err
	}
	if len(attachmentIDs) > 0 {
		if err := s.attachments.CheckBatch(ctx, attachmentIDs); err != nil {
			return attachmentError(err)
		}
	}

	event, err := s.rooms.SendMessage(ctx, roomID, user.ID, request.Body, request.ClientToken)
	if err != nil {
		return roomError(err)
	}

	// Bound after the event exists, since the binding names it. Scoped to this
	// uploader and room, so one person cannot attach another's upload.
	if len(attachmentIDs) > 0 {
		if _, err := s.attachments.Bind(ctx, event.ID, roomID, user.ID, attachmentIDs); err != nil {
			return attachmentError(err)
		}
	}

	// Delivered after it is stored, so a client that misses the live frame
	// still finds the message on reconnection.
	s.publishEvent(ctx, event)

	return c.JSON(http.StatusCreated, s.withAttachments(ctx, messageResponseOf(event)))
}

type editMessageRequest struct {
	Body string `json:"body"`
}

func (s *Server) handleEditMessage(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request editMessageRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	event, err := s.rooms.EditMessage(ctx, roomID, user.ID, c.Param("eventId"), request.Body)
	if err != nil {
		return roomError(err)
	}

	s.publishEvent(ctx, event)
	return c.JSON(http.StatusCreated, messageResponseOf(event))
}

func (s *Server) handleDeleteMessage(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// Deleting someone else's message needs the moderation permission; the
	// service enforces authorship when it is absent.
	moderator := currentPermissions(c).AllowsChannel(authz.PermMessageDeleteAny, roomID).Allowed

	event, err := s.rooms.DeleteMessage(ctx, roomID, user.ID, c.Param("eventId"), moderator)
	if err != nil {
		return roomError(err)
	}

	s.publishEvent(ctx, event)
	return c.JSON(http.StatusCreated, messageResponseOf(event))
}

func (s *Server) handleRoomMembers(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	members, err := s.rooms.Members(c.Request().Context(), roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	response := make([]map[string]any, 0, len(members))
	for _, member := range members {
		response = append(response, map[string]any{
			"id":          member.ID.String(),
			"username":    member.Username,
			"displayName": member.DisplayName,
			"address":     member.Username + "@" + s.serverName(),
			"joinedAt":    member.JoinedAt,
		})
	}
	return c.JSON(http.StatusOK, response)
}

// roomError maps a rooms failure to a response.
//
// A room the requester may not see is reported as missing, identically to one
// that does not exist, so its existence cannot be probed.
func roomError(err error) error {
	switch {
	case errors.Is(err, rooms.ErrRoomNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	case errors.Is(err, rooms.ErrNotAMember):
		return echo.NewHTTPError(http.StatusForbidden, "you are not a member of this room")
	case errors.Is(err, rooms.ErrArchived):
		return echo.NewHTTPError(http.StatusConflict, "this room is archived")
	case errors.Is(err, rooms.ErrNotTheAuthor):
		return echo.NewHTTPError(http.StatusForbidden, "only the author may change this message")
	case errors.Is(err, rooms.ErrSlugTaken):
		return echo.NewHTTPError(http.StatusConflict, "that room address is already taken")
	case errors.Is(err, rooms.ErrInvalidSlug),
		errors.Is(err, rooms.ErrEmptyMessage),
		errors.Is(err, rooms.ErrMessageTooLong):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	default:
		return err
	}
}
