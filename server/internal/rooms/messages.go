package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// MaxMessageLength bounds a single message.
//
// Generous for prose while keeping one message from filling a page of a
// timeline or a notification payload.
const MaxMessageLength = 8000

// ErrMessageTooLong reports a message beyond the limit.
var ErrMessageTooLong = errors.New("message is too long")

// ErrEmptyMessage reports a message with no content.
var ErrEmptyMessage = errors.New("message is empty")

// append signs an event, allocates its position, and stores it.
//
// The sequence number is allocated inside the same call that inserts, so two
// concurrent senders cannot receive the same position.
func (s *Service) append(
	ctx context.Context,
	room dbgen.Room,
	sender dbgen.User,
	eventType events.Type,
	content json.RawMessage,
	clientToken string,
) (dbgen.Event, error) {
	sequence, err := s.store.NextRoomSequence(ctx, room.ID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("allocate sequence: %w", err)
	}

	nonce, err := events.NewNonce()
	if err != nil {
		return dbgen.Event{}, err
	}

	event := events.Event{
		Room:      s.RoomAddress(room),
		Sender:    s.UserAddress(sender.Username),
		Origin:    s.serverName,
		Type:      eventType,
		CreatedAt: events.Now(),
		Nonce:     nonce,
		Content:   content,
	}
	if err := s.keyring.Sign(&event); err != nil {
		return dbgen.Event{}, fmt.Errorf("sign event: %w", err)
	}

	stored, err := s.store.AppendEvent(ctx, dbgen.AppendEventParams{
		ID:            event.ID,
		RoomID:        room.ID,
		Sequence:      sequence,
		SenderID:      &sender.ID,
		SenderAddress: event.Sender,
		RoomAddress:   event.Room,
		OriginServer:  event.Origin,
		Type:          string(event.Type),
		Content:       content,
		// The digest of what was signed, before Postgres normalises the
		// content and makes it un-hashable back to the same value.
		ContentHash: pointerTo(events.HashContent(content)),
		CreatedAt:   event.CreatedAt,
		Nonce:       event.Nonce,
		Signature:   event.Signature,
		KeyID:       event.KeyID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("append event: %w", err)
	}

	if clientToken != "" {
		if err := s.store.RecordIdempotentSubmission(ctx, dbgen.RecordIdempotentSubmissionParams{
			SenderID:    sender.ID,
			ClientToken: clientToken,
			EventID:     stored.ID,
		}); err != nil {
			return dbgen.Event{}, fmt.Errorf("record submission: %w", err)
		}
	}

	return stored, nil
}

// SendMessage posts a message to a room.
//
// A client token makes the submission idempotent: a client that retries
// because it did not see the outcome gets the original event back rather than
// creating a second one.
func (s *Service) SendMessage(
	ctx context.Context,
	roomID, senderID uuid.UUID,
	body string,
	clientToken string,
) (dbgen.Event, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return dbgen.Event{}, ErrEmptyMessage
	}
	if len(trimmed) > MaxMessageLength {
		return dbgen.Event{}, fmt.Errorf("%w: limit is %d characters", ErrMessageTooLong, MaxMessageLength)
	}

	// Checked before any work, so a retry costs nothing and cannot produce a
	// second event.
	if clientToken != "" {
		existing, err := s.store.FindIdempotentSubmission(ctx, dbgen.FindIdempotentSubmissionParams{
			SenderID:    senderID,
			ClientToken: clientToken,
		})
		if err == nil {
			return s.store.GetEventByID(ctx, existing)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return dbgen.Event{}, fmt.Errorf("check submission: %w", err)
		}
	}

	room, err := s.RequireMembership(ctx, roomID, senderID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	sender, err := s.store.GetUserByID(ctx, senderID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read sender: %w", err)
	}

	content, err := json.Marshal(map[string]any{"body": trimmed})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode message: %w", err)
	}

	return s.append(ctx, room, sender, events.TypeMessageSent, content, clientToken)
}

// EditMessage replaces a message's body.
//
// The edit is a new event rather than a change to the original: the log is
// append-only, and the original event's signature must keep verifying.
func (s *Service) EditMessage(
	ctx context.Context,
	roomID, senderID uuid.UUID,
	targetEventID, body string,
) (dbgen.Event, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return dbgen.Event{}, ErrEmptyMessage
	}
	if len(trimmed) > MaxMessageLength {
		return dbgen.Event{}, fmt.Errorf("%w: limit is %d characters", ErrMessageTooLong, MaxMessageLength)
	}

	room, err := s.RequireMembership(ctx, roomID, senderID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	target, err := s.store.GetEventByID(ctx, targetEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read message: %w", err)
	}
	if target.RoomID != roomID {
		// The message is not in this room; reported as missing rather than
		// confirming it exists elsewhere.
		return dbgen.Event{}, ErrRoomNotFound
	}
	// Editing someone else's message needs a moderation permission, which the
	// caller checks; this service enforces only authorship.
	if target.SenderID == nil || *target.SenderID != senderID {
		return dbgen.Event{}, ErrNotTheAuthor
	}

	sender, err := s.store.GetUserByID(ctx, senderID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read sender: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"body":     trimmed,
		"replaces": targetEventID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode edit: %w", err)
	}

	return s.append(ctx, room, sender, events.TypeMessageEdited, content, "")
}

// DeleteMessage removes a message's content.
//
// The event stays in the log, redacted: participants see that a message was
// removed rather than the conversation appearing never to have contained it,
// and the surrounding events stay verifiable.
func (s *Service) DeleteMessage(
	ctx context.Context,
	roomID, actorID uuid.UUID,
	targetEventID string,
	moderator bool,
) (dbgen.Event, error) {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Event{}, err
	}

	target, err := s.store.GetEventByID(ctx, targetEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read message: %w", err)
	}
	if target.RoomID != roomID {
		return dbgen.Event{}, ErrRoomNotFound
	}

	authored := target.SenderID != nil && *target.SenderID == actorID
	if !authored && !moderator {
		return dbgen.Event{}, ErrNotTheAuthor
	}

	if err := s.store.RedactEvent(ctx, dbgen.RedactEventParams{
		ID:         targetEventID,
		RedactedBy: &actorID,
	}); err != nil {
		return dbgen.Event{}, fmt.Errorf("redact message: %w", err)
	}

	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read actor: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"redacts": targetEventID,
		// Recorded because a moderator removing someone else's message is a
		// different act from an author removing their own.
		"byModerator": !authored,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode deletion: %w", err)
	}

	return s.append(ctx, room, actor, events.TypeMessageDeleted, content, "")
}

// Timeline returns a room's most recent events.
func (s *Service) Timeline(ctx context.Context, roomID, userID uuid.UUID, limit int32) ([]dbgen.Event, error) {
	if _, err := s.Visible(ctx, roomID, userID); err != nil {
		return nil, err
	}

	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// Replies are excluded: they live in their thread, not the channel, unless
	// a client explicitly asks for the thread.
	timeline, err := s.store.ListRoomTimelineExcludingReplies(ctx, dbgen.ListRoomTimelineExcludingRepliesParams{
		RoomID: roomID,
		Limit:  limit,
	})
	if err != nil {
		return nil, fmt.Errorf("read timeline: %w", err)
	}

	// The query returns newest first for the limit to mean "most recent";
	// callers want oldest first for display.
	for left, right := 0, len(timeline)-1; left < right; left, right = left+1, right-1 {
		timeline[left], timeline[right] = timeline[right], timeline[left]
	}

	return timeline, nil
}

// EventsSince returns everything a client missed, for replay after a
// reconnection.
func (s *Service) EventsSince(
	ctx context.Context,
	roomID, userID uuid.UUID,
	afterSequence int64,
) ([]dbgen.Event, error) {
	if _, err := s.Visible(ctx, roomID, userID); err != nil {
		return nil, err
	}

	latest, err := s.store.LatestRoomSequence(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("read latest sequence: %w", err)
	}

	missed, err := s.store.ListRoomEvents(ctx, dbgen.ListRoomEventsParams{
		RoomID:     roomID,
		Sequence:   afterSequence,
		Sequence_2: latest,
	})
	if err != nil {
		return nil, fmt.Errorf("read missed events: %w", err)
	}

	return missed, nil
}

/*
Event reads one event by its identifier.

For the fanout between nodes: an announcement carries an identifier rather than
the event, because a notification cannot carry a long message, so the node that
hears it reads the event from the database they share.

No membership check, deliberately. The caller is the hub, which decides who sees
this by asking who is in the room and who is subscribed — the same decision it
makes for an event stored on its own node. A check here would be a second,
different answer to a question already answered.
*/
func (s *Service) Event(ctx context.Context, eventID string) (dbgen.Event, error) {
	event, err := s.store.GetEventByID(ctx, eventID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read event: %w", err)
	}
	return event, nil
}

// pointerTo is for the optional columns sqlc generates as pointers.
func pointerTo[T any](value T) *T { return &value }
