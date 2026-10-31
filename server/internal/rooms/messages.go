package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/actions"
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
	return s.appendIn(ctx, room, sender, eventType, content, clientToken, nil)
}

/*
appendIn is append for an event that belongs to a thread: a reply, or anything
acting on one — its edit, its deletion, a program replacing its controls, a
press on it. Carrying the root is what keeps all of these in the thread rather
than the channel, live and on replay alike. threadRootID nil is the channel.
*/
func (s *Service) appendIn(
	ctx context.Context,
	room dbgen.Room,
	sender dbgen.User,
	eventType events.Type,
	content json.RawMessage,
	clientToken string,
	threadRootID *string,
) (dbgen.Event, error) {
	return s.appendAs(ctx, room, &sender.ID, s.UserAddress(sender.Username), eventType, content, clientToken, threadRootID)
}

/*
AppendIn appends to a room's log as part of a transaction the caller holds, so
a change and the event recording it are written together or not at all — what
projects do for issues, and the calendar for events.

The sender's account may be nil for something the server did by itself.

Nothing is told of the event here: until the caller commits, nobody else can
read it or what it records, and a webhook queued now would look for the
change and not find it. The caller passes it to Told once committed.
*/
func (s *Service) AppendIn(
	ctx context.Context,
	q Store,
	room dbgen.Room,
	senderID *uuid.UUID,
	senderAddress string,
	eventType events.Type,
	content json.RawMessage,
) (dbgen.Event, error) {
	within := *s
	within.store = q
	within.appended = nil
	return within.appendAs(ctx, room, senderID, senderAddress, eventType, content, "", nil)
}

/*
appendAs is append for a sender that may have no account behind it — an
incoming webhook, which posts as itself rather than as anybody. The address is
still recorded and signed, so the event says who it is from; only the account
reference is absent.
*/
func (s *Service) appendAs(
	ctx context.Context,
	room dbgen.Room,
	senderID *uuid.UUID,
	senderAddress string,
	eventType events.Type,
	content json.RawMessage,
	clientToken string,
	threadRootID *string,
) (dbgen.Event, error) {
	sequence, err := s.nextSequence(ctx, room)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("allocate sequence: %w", err)
	}

	nonce, err := events.NewNonce()
	if err != nil {
		return dbgen.Event{}, err
	}

	event := events.Event{
		Room:      s.RoomAddress(room),
		Sender:    senderAddress,
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
		SenderID:      senderID,
		SenderAddress: event.Sender,
		RoomAddress:   event.Room,
		OriginServer:  event.Origin,
		Type:          string(event.Type),
		Content:       content,
		// The digest of what was signed, before Postgres normalises the
		// content and makes it un-hashable back to the same value.
		ContentHash:  pointerTo(events.HashContent(content)),
		CreatedAt:    event.CreatedAt,
		Nonce:        event.Nonce,
		Signature:    event.Signature,
		KeyID:        event.KeyID,
		ThreadRootID: threadRootID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("append event: %w", err)
	}
	s.told(ctx, stored)

	// Idempotency is per account, so a sender without one has none.
	if clientToken != "" && senderID != nil {
		if err := s.store.RecordIdempotentSubmission(ctx, dbgen.RecordIdempotentSubmissionParams{
			SenderID:    *senderID,
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
	offered ...actions.Action,
) (dbgen.Event, error) {
	return s.SendMessageWithFiles(ctx, roomID, senderID, body, clientToken, nil, offered...)
}

/*
SendMessageWithFiles posts a message that carries files, described in its
content.

The text may be empty when there are files: a photograph needs no caption to be
worth sending. Neither text nor a file is still nothing, and refused.
*/
func (s *Service) SendMessageWithFiles(
	ctx context.Context,
	roomID, senderID uuid.UUID,
	body string,
	clientToken string,
	files []FileRef,
	offered ...actions.Action,
) (dbgen.Event, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" && len(files) == 0 {
		return dbgen.Event{}, ErrEmptyMessage
	}

	/*
		The buttons, checked before anything is written.

		Refusing here rather than at the press is the point: a message that
		reached a channel and then failed when somebody touched it has already
		been read, and somebody has already decided the button was worth
		trusting.
	*/
	if err := actions.Check(offered); err != nil {
		return dbgen.Event{}, err
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

	payload := map[string]any{"body": trimmed}
	// Absent rather than empty when there are none, so an ordinary message's
	// content is exactly what it was before actions existed.
	if len(offered) > 0 {
		payload["actions"] = offered
	}
	if len(files) > 0 {
		payload["attachments"] = files
	}
	if err := s.markIfProgram(ctx, sender, payload); err != nil {
		return dbgen.Event{}, err
	}

	content, err := json.Marshal(payload)
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
	return s.EditMessageAs(ctx, roomID, senderID, targetEventID, body, false)
}

/*
EditMessageAs replaces a message's body, by its author or — when moderator is
true — by somebody the caller has established may edit anybody's.

The edit is sent by whoever made it, never by the author: the event's sender is
the moderator, and its content says it was a moderator's doing, so every reader
sees that somebody other than the author changed what the author is shown to
have said. A moderator may edit only a message, not a membership or a room's
own events, and not one already deleted.
*/
func (s *Service) EditMessageAs(
	ctx context.Context,
	roomID, senderID uuid.UUID,
	targetEventID, body string,
	moderator bool,
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
	// caller checks; this service enforces authorship when it is absent.
	authored := target.SenderID != nil && *target.SenderID == senderID
	if !authored {
		if !moderator {
			return dbgen.Event{}, ErrNotTheAuthor
		}
		// Only what somebody said, and only while it can still be read: a
		// deleted message edited back into view would undo the deletion.
		if target.Type != string(events.TypeMessageSent) || target.RedactedAt != nil {
			return dbgen.Event{}, ErrNotTheAuthor
		}
	}

	sender, err := s.store.GetUserByID(ctx, senderID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read sender: %w", err)
	}

	edit := map[string]any{
		"body":     trimmed,
		"replaces": targetEventID,
	}
	// Recorded because a moderator changing somebody else's words is a
	// different act from an author correcting their own, as with deletion.
	if !authored {
		edit["byModerator"] = true
	}
	content, err := json.Marshal(edit)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode edit: %w", err)
	}

	return s.appendIn(ctx, room, sender, events.TypeMessageEdited, content, "", target.ThreadRootID)
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

	/*
		On a replica, the redaction is complete and stays here.

		The content is gone from this server, which is the whole of what a
		moderator here can decide and the whole of what the spec asks: local
		authority applies to federated content without the origin's
		cooperation. What cannot follow is a deletion *event*, because writing
		one would take a position in an order this server does not own.

		So the origin's copy is untouched, deliberately. A server cannot
		compel another to forget something, and pretending otherwise — by
		sending a request the far side is free to ignore — would tell a
		moderator their decision travelled when it did not.
	*/
	if room.HomeServer != "" && !strings.EqualFold(room.HomeServer, s.serverName) {
		return target, ErrRedactedLocally
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

	return s.appendIn(ctx, room, actor, events.TypeMessageDeleted, content, "", target.ThreadRootID)
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
