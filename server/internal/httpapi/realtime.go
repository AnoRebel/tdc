package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// maxReplayEvents bounds how far back a reconnecting client may be replayed.
//
// Beyond this the client is told to resynchronise instead. Replaying an
// unbounded history would let a client that has been away for a month pull the
// entire room through one connection, which is slow for them and expensive for
// everyone else.
const maxReplayEvents = 500

// roomReplayer adapts the rooms service to what the realtime server needs.
//
// It exists so the realtime package depends on an interface it defines rather
// than on the rooms package, keeping the transport independent of the domain.
type roomReplayer struct {
	rooms       *rooms.Service
	attachments *attachments.Service
}

// CanSubscribe reports whether a user may receive a room's events.
func (r roomReplayer) CanSubscribe(ctx context.Context, roomID, userID uuid.UUID) (bool, error) {
	// Membership rather than visibility: a public room is visible to everyone
	// on the server, but only members receive its live events.
	if _, err := r.rooms.RequireMembership(ctx, roomID, userID); err != nil {
		// A refusal is not an error here; the caller only needs to know the
		// answer is no, and must not learn whether the room exists.
		return false, nil
	}
	return true, nil
}

// EventsSince returns what a client missed, reporting whether the gap was
// small enough to replay.
func (r roomReplayer) EventsSince(
	ctx context.Context,
	roomID, userID uuid.UUID,
	afterSequence int64,
) ([]realtime.Frame, bool, error) {
	missed, err := r.rooms.EventsSince(ctx, roomID, userID, afterSequence)
	if err != nil {
		return nil, false, err
	}

	if len(missed) > maxReplayEvents {
		// Too far behind. Saying so is essential: quietly sending a truncated
		// stream would leave the client with a hole it cannot detect.
		return nil, false, nil
	}

	frames := make([]realtime.Frame, 0, len(missed))
	for _, event := range missed {
		frame, err := eventFrame(event, r.attachmentsFor(ctx, event.ID))
		if err != nil {
			return nil, false, err
		}
		frames = append(frames, frame)
	}

	return frames, true, nil
}

// eventFrame renders a stored event as a frame for the wire.
//
// Attachments are passed in rather than looked up here, so that the two callers
// — live delivery and replay after a reconnection — decide for themselves how
// to fetch them. A frame without them would show a message arriving with its
// files missing until the page was reloaded.
func eventFrame(event dbgen.Event, files []attachmentResponse) (realtime.Frame, error) {
	body := map[string]any{
		"id":        event.ID,
		"type":      event.Type,
		"sender":    event.SenderAddress,
		"content":   json.RawMessage(event.Content),
		"createdAt": event.CreatedAt,
		"redacted":  event.RedactedAt != nil,
	}
	if len(files) > 0 {
		body["attachments"] = files
	}
	// A reply names the thread it belongs to, so a client can keep it out of
	// the channel timeline rather than showing it in both places.
	if event.ThreadRootID != nil {
		body["threadRoot"] = *event.ThreadRootID
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return realtime.Frame{}, fmt.Errorf("encode event frame: %w", err)
	}

	return realtime.Frame{
		Type:     realtime.FrameEvent,
		Room:     event.RoomAddress,
		RoomID:   event.RoomID.String(),
		Sequence: event.Sequence,
		Payload:  payload,
	}, nil
}

// handleWebSocket upgrades an authenticated request to a live connection.
func (s *Server) handleWebSocket(c *echo.Context) error {
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication is required")
	}

	// Same-origin is the normal deployment, so an empty list means only this
	// server's own origin. A browser applies no same-origin policy to
	// WebSockets, so this list is what stops another site opening one.
	origins := s.config.AllowedOrigins
	if len(origins) == 0 {
		origins = []string{s.serverName()}
	}

	return s.realtime.Handle(c.Response(), c.Request(), user.ID, origins)
}

// publishEvent delivers a stored event to everyone entitled to see it.
//
// Failure to deliver live is logged rather than returned: the event is already
// in the log, so a client that misses it will receive it on reconnection. The
// send must not fail because a fanout did.
func (s *Server) publishEvent(ctx context.Context, event dbgen.Event) {
	frame, err := eventFrame(event, roomReplayer{attachments: s.attachments}.attachmentsFor(ctx, event.ID))
	if err != nil {
		s.log.Error("cannot render event for delivery",
			"eventId", event.ID, "error", err)
		return
	}
	s.hub.Publish(ctx, event.RoomID, frame)
}

// attachmentsFor collects a message's attachments for a live frame.
//
// A failure returns nothing rather than propagating: the message itself is what
// matters most, and withholding it because a file could not be listed would be
// the worse outcome. The attachments appear on the next timeline fetch.
func (r roomReplayer) attachmentsFor(ctx context.Context, eventID string) []attachmentResponse {
	files, err := r.attachments.ForEvent(ctx, eventID)
	if err != nil || len(files) == 0 {
		return nil
	}

	response := make([]attachmentResponse, 0, len(files))
	for _, file := range files {
		response = append(response, attachmentResponseOf(file))
	}
	return response
}
