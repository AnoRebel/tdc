package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/actions"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Answering a press.

A program told that somebody pressed a button has three ways to answer: post
an ordinary message, which needs nothing new; replace the message's text and
controls where everybody sees them; or reply to the person who pressed and
nobody else. The last two live here, along with what a program may remember
about one person between one step of an exchange and the next.
*/

// ErrNotAMessage reports an update aimed at something that is not a message.
var ErrNotAMessage = errors.New("only a message can be updated")

// ErrInvocationExpired reports a private reply to a press too old to answer.
var ErrInvocationExpired = errors.New("that press is too old to answer privately")

// ErrNotAPress reports a private reply to something that is not a press.
var ErrNotAPress = errors.New("that is not a press anybody made")

// ErrContextTooLarge reports context bigger than a program may keep.
var ErrContextTooLarge = errors.New("that is more than an integration may remember")

/*
PrivateReplyWindow is how long after a press its program may answer the
presser privately.

Fifteen minutes. A private reply is part of answering somebody, and somebody
who pressed a button an hour ago is not waiting for one — while a program able
to message any presser at any time would have found a way to reach people
privately that nobody agreed to.
*/
const PrivateReplyWindow = 15 * time.Minute

// MaxContextBytes bounds what a program remembers about one exchange, matching
// the database's own constraint so the refusal is a sentence rather than a
// constraint violation.
const MaxContextBytes = 16384

// Controls are a message's text and controls as they currently stand.
type Controls struct {
	// Version is zero for a message as it was posted, and counts updates.
	Version int32
	Body    string
	Actions []actions.Action
}

/*
Controls reads what a message currently offers.

The latest update if there is one, otherwise what was posted. One read either
way, because the update row is the current version rather than a history to
replay.
*/
func (s *Service) Controls(ctx context.Context, message dbgen.Event) (Controls, error) {
	updated, err := s.store.GetMessageInteraction(ctx, message.ID)
	if err == nil {
		var offered []actions.Action
		if err := json.Unmarshal(updated.Actions, &offered); err != nil {
			return Controls{}, fmt.Errorf("read the updated controls: %w", err)
		}
		return Controls{Version: updated.Version, Body: updated.Body, Actions: offered}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Controls{}, fmt.Errorf("read the message's controls: %w", err)
	}

	var posted struct {
		Body    string           `json:"body"`
		Actions []actions.Action `json:"actions"`
	}
	// A message whose content cannot be read offers nothing, rather than
	// failing the press with an error that means nothing to whoever pressed.
	_ = json.Unmarshal(message.Content, &posted)
	return Controls{Version: 0, Body: posted.Body, Actions: posted.Actions}, nil
}

// Update is a program replacing a message's text and controls.
type Update struct {
	RoomID   uuid.UUID
	AuthorID uuid.UUID
	Message  string
	// Expected is the version being replaced. The update is applied only if
	// that is still the current one.
	Expected int32
	Body     string
	Actions  []actions.Action
}

/*
UpdateMessage replaces a message's text and controls, atomically, if nothing
else has replaced them first.

Only the author may: the message is the program's question, and nobody else
gets to change what it asks. The new controls are checked exactly as posted
ones are, before anything is written.

The version comparison is the database's, not this function's: an update from
version zero inserts and conflicts with any other insert, and every later one
is an UPDATE conditioned on the version. Two responses racing to answer one
press produce one update and one ErrStaleControls, however they interleave.

The update is then appended to the room's log, which is how every reader's
client — live or reconnecting later — comes to see the new controls.
*/
func (s *Service) UpdateMessage(ctx context.Context, update Update) (dbgen.Event, Controls, error) {
	body := strings.TrimSpace(update.Body)
	if body == "" {
		return dbgen.Event{}, Controls{}, ErrEmptyMessage
	}
	if len(body) > MaxMessageLength {
		return dbgen.Event{}, Controls{}, fmt.Errorf("%w: limit is %d characters", ErrMessageTooLong, MaxMessageLength)
	}
	if err := actions.Check(update.Actions); err != nil {
		return dbgen.Event{}, Controls{}, err
	}

	room, err := s.RequireMembership(ctx, update.RoomID, update.AuthorID)
	if err != nil {
		return dbgen.Event{}, Controls{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, Controls{}, ErrArchived
	}

	target, err := s.store.GetEventByID(ctx, update.Message)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, Controls{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, Controls{}, fmt.Errorf("read the message: %w", err)
	}
	if target.RoomID != update.RoomID {
		return dbgen.Event{}, Controls{}, ErrRoomNotFound
	}
	if target.Type != string(events.TypeMessageSent) || target.RedactedAt != nil {
		return dbgen.Event{}, Controls{}, ErrNotAMessage
	}
	if target.SenderID == nil || *target.SenderID != update.AuthorID {
		return dbgen.Event{}, Controls{}, ErrNotTheAuthor
	}

	offered := update.Actions
	if offered == nil {
		// Stored as an empty list rather than null, so "no controls" and
		// "unreadable controls" are never the same thing.
		offered = []actions.Action{}
	}
	encoded, err := json.Marshal(offered)
	if err != nil {
		return dbgen.Event{}, Controls{}, fmt.Errorf("encode the controls: %w", err)
	}

	var stored dbgen.MessageInteraction
	if update.Expected == 0 {
		stored, err = s.store.CreateMessageInteraction(ctx, dbgen.CreateMessageInteractionParams{
			EventID:   update.Message,
			RoomID:    update.RoomID,
			Body:      body,
			Actions:   encoded,
			UpdatedBy: &update.AuthorID,
		})
	} else {
		stored, err = s.store.AdvanceMessageInteraction(ctx, dbgen.AdvanceMessageInteractionParams{
			EventID:   update.Message,
			Version:   update.Expected,
			Body:      body,
			Actions:   encoded,
			UpdatedBy: &update.AuthorID,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// Somebody else's update landed first, or the version named was never
		// current. Either way this response was written against a view that
		// no longer exists.
		return dbgen.Event{}, Controls{}, ErrStaleControls
	}
	if err != nil {
		return dbgen.Event{}, Controls{}, fmt.Errorf("update the message: %w", err)
	}

	author, err := s.store.GetUserByID(ctx, update.AuthorID)
	if err != nil {
		return dbgen.Event{}, Controls{}, fmt.Errorf("read the author: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"updates": update.Message,
		"version": stored.Version,
		"body":    body,
		"actions": offered,
	})
	if err != nil {
		return dbgen.Event{}, Controls{}, fmt.Errorf("encode the update: %w", err)
	}

	/*
		Written after the version moved, deliberately. If appending fails the
		controls have changed without the room being told, and the next read
		of the timeline shows the old ones — a press through that view is
		refused as stale, which is the safe direction to be wrong in. The
		reverse order could announce an update that then lost its race.
	*/
	event, err := s.append(ctx, room, author, events.TypeMessageUpdated, content, "")
	if err != nil {
		return dbgen.Event{}, Controls{}, err
	}

	return event, Controls{Version: stored.Version, Body: body, Actions: offered}, nil
}

// Invocation is a press, as its program answers it.
type Invocation struct {
	Room dbgen.Room
	// Press is the action.pressed event.
	Press dbgen.Event
	// Message is the event the press was made on.
	Message string
	// Invoker is who pressed.
	Invoker dbgen.User
}

/*
PrivateReplyTarget finds who a program may answer privately, for one press.

Only the author of the pressed message may, only within PrivateReplyWindow of
the press, and only while the presser is still in the room. The reply goes to
the presser and to nobody else: the program names the press, never a person,
so there is no way to use this to reach somebody who did not ask.
*/
func (s *Service) PrivateReplyTarget(
	ctx context.Context,
	roomID, authorID uuid.UUID,
	pressEventID string,
) (Invocation, error) {
	room, err := s.RequireMembership(ctx, roomID, authorID)
	if err != nil {
		return Invocation{}, err
	}

	pressed, err := s.store.GetEventByID(ctx, pressEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invocation{}, ErrNotAPress
	}
	if err != nil {
		return Invocation{}, fmt.Errorf("read the press: %w", err)
	}
	if pressed.RoomID != roomID || pressed.Type != string(events.TypeActionPressed) || pressed.SenderID == nil {
		return Invocation{}, ErrNotAPress
	}

	var content struct {
		Pressed string `json:"pressed"`
	}
	if err := json.Unmarshal(pressed.Content, &content); err != nil || content.Pressed == "" {
		return Invocation{}, ErrNotAPress
	}

	message, err := s.store.GetEventByID(ctx, content.Pressed)
	if err != nil {
		return Invocation{}, ErrNotAPress
	}
	if message.SenderID == nil || *message.SenderID != authorID {
		return Invocation{}, ErrNotTheAuthor
	}

	// Event times are milliseconds since the epoch, an absolute instant.
	if time.Since(time.UnixMilli(pressed.CreatedAt)) > PrivateReplyWindow {
		return Invocation{}, ErrInvocationExpired
	}

	member, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: roomID,
		UserID: *pressed.SenderID,
	})
	if err != nil {
		return Invocation{}, fmt.Errorf("check the presser is still here: %w", err)
	}
	if !member {
		// Gone from the room. Told as a press nobody can be answered for,
		// because whether they left is theirs to tell.
		return Invocation{}, ErrNotAPress
	}

	invoker, err := s.store.GetUserByID(ctx, *pressed.SenderID)
	if err != nil {
		return Invocation{}, fmt.Errorf("read who pressed: %w", err)
	}

	return Invocation{Room: room, Press: pressed, Message: content.Pressed, Invoker: invoker}, nil
}

/*
Context reads what a program remembers about one person in one room.

The program is always the caller — there is no parameter by which one program
could ask for another's — and nil with no error means it remembers nothing.
*/
func (s *Service) Context(ctx context.Context, integrationID, roomID, userID uuid.UUID) (json.RawMessage, error) {
	if _, err := s.RequireMembership(ctx, roomID, integrationID); err != nil {
		return nil, err
	}

	row, err := s.store.GetIntegrationContext(ctx, dbgen.GetIntegrationContextParams{
		IntegrationID: integrationID,
		RoomID:        roomID,
		UserID:        userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the context: %w", err)
	}
	return row.State, nil
}

/*
RememberContext stores what a program wants to recall at the next step of an
exchange with somebody.

Both must be in the room: the program because it is its conversation, and the
person because there is no exchange with somebody who is not there to have it.
*/
func (s *Service) RememberContext(
	ctx context.Context,
	integrationID, roomID, userID uuid.UUID,
	state json.RawMessage,
) error {
	if len(state) > MaxContextBytes {
		return fmt.Errorf("%w: at most %d bytes", ErrContextTooLarge, MaxContextBytes)
	}
	if !json.Valid(state) {
		return fmt.Errorf("%w: it is not JSON", ErrContextTooLarge)
	}

	if _, err := s.RequireMembership(ctx, roomID, integrationID); err != nil {
		return err
	}
	member, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{RoomID: roomID, UserID: userID})
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !member {
		return ErrNotAMember
	}

	if _, err := s.store.PutIntegrationContext(ctx, dbgen.PutIntegrationContextParams{
		IntegrationID: integrationID,
		RoomID:        roomID,
		UserID:        userID,
		State:         state,
	}); err != nil {
		return fmt.Errorf("store the context: %w", err)
	}
	return nil
}

// ForgetContext ends an exchange, discarding what the program remembered.
func (s *Service) ForgetContext(ctx context.Context, integrationID, roomID, userID uuid.UUID) error {
	if _, err := s.store.DeleteIntegrationContext(ctx, dbgen.DeleteIntegrationContextParams{
		IntegrationID: integrationID,
		RoomID:        roomID,
		UserID:        userID,
	}); err != nil {
		return fmt.Errorf("forget the context: %w", err)
	}
	return nil
}

// ForgetAllContext discards everything a program remembered, for when the
// program itself is removed.
func (s *Service) ForgetAllContext(ctx context.Context, integrationID uuid.UUID) error {
	if _, err := s.store.DeleteIntegrationContextForIntegration(ctx, integrationID); err != nil {
		return fmt.Errorf("forget the program's context: %w", err)
	}
	return nil
}
