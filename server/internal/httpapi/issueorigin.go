package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
originResponse is the conversation an issue was made from, as far as the
person looking may know.

Two shapes and only two. Somebody who can read the room it came from sees the
room and the message — as the message reads there now, edited or deleted. Anybody
else sees that the issue came from a conversation, and nothing more: not the
room's name, not who wrote it, not when, and not whether it still exists. A
private room and a message since deleted from one look exactly the same from
outside, because telling them apart would itself say something about a room the
reader cannot see.
*/
type originResponse struct {
	// Disclosed says whether the reader may see where this came from. When
	// false, nothing else is present.
	Disclosed bool           `json:"disclosed"`
	RoomID    string         `json:"roomId,omitempty"`
	RoomName  string         `json:"roomName,omitempty"`
	EventID   string         `json:"eventId,omitempty"`
	Message   *originMessage `json:"message,omitempty"`
}

// originMessage is the message itself, for somebody who may read it.
type originMessage struct {
	Sender string    `json:"sender"`
	SentAt time.Time `json:"sentAt"`
	// Body is what it says now: the latest edit if it was edited, and absent
	// if it was removed.
	Body    string `json:"body,omitempty"`
	Edited  bool   `json:"edited,omitempty"`
	Removed bool   `json:"removed,omitempty"`
}

/*
originOf resolves an issue's origin for the person asking.

The room's own rule decides: rooms.Visible, the same check a read of the
channel itself makes, so the reference can never show somebody what the
channel would not. That check happens before the message is read at all — for
anybody it refuses, the message is never fetched, and there is no second path
whose cost could differ with what the message is.
*/
func (s *Server) originOf(ctx context.Context, c *echo.Context, access projects.Access, issue dbgen.Issue) (*originResponse, error) {
	origin, err := s.projects.OriginOf(ctx, access, issue)
	if err != nil || origin == nil {
		return nil, err
	}

	user, _ := currentUser(c)
	room, err := s.rooms.Visible(ctx, origin.RoomID, user.ID)
	if err != nil {
		// Whatever the reason — private, gone, never existed — the answer is
		// the same, and says only that there was a conversation.
		return &originResponse{Disclosed: false}, nil
	}

	response := &originResponse{
		Disclosed: true,
		RoomID:    room.ID.String(),
		RoomName:  room.Name,
		EventID:   origin.EventID,
	}

	event, err := s.queries.GetEventByID(ctx, origin.EventID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && event.RoomID != room.ID) {
		// The room is visible and the message is not in it: it went with a
		// retention sweep or an erasure. Removed, as the channel would say.
		response.Message = &originMessage{Removed: true}
		return response, nil
	}
	if err != nil {
		return nil, err
	}

	message := &originMessage{
		Sender:  event.SenderAddress,
		SentAt:  time.UnixMilli(event.CreatedAt).UTC(),
		Removed: event.RedactedAt != nil,
	}
	if !message.Removed {
		message.Body = bodyOf(event.Content)
		edit, err := s.queries.LatestMessageEdit(ctx, dbgen.LatestMessageEditParams{
			RoomID: room.ID, EventID: event.ID,
		})
		if err == nil {
			message.Body = bodyOf(edit.Content)
			message.Edited = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	response.Message = message
	return response, nil
}

// bodyOf reads a message's text out of its content.
func bodyOf(content []byte) string {
	var message struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(content, &message)
	return message.Body
}
