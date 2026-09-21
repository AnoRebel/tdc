package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/actions"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Pressing a button on a message.

The press becomes an event in the room's log, which is how it reaches the
program that posted the message: over the live connection it already holds,
through the same delivery every other event uses. A separate mechanism would
mean separate retries, separate failures and a separate way of being wrong.
*/

// ErrNoSuchAction reports a press of a button the message does not offer.
var ErrNoSuchAction = errors.New("that message offers no such action")

// ErrAlreadyPressed reports a second press of something offered once.
var ErrAlreadyPressed = errors.New("that has already been answered")

// ErrActionExpired reports a press after the button stopped being offered.
var ErrActionExpired = errors.New("that is no longer offered")

/*
PressAction records somebody pressing a button, and returns the event saying so.

The order is deliberate: the press is written first and the event appended
second. A press recorded without its event is a button that looks used and
tells nobody, which somebody can retry; an event announced without its press
recorded is a program told twice that a one-shot choice was made, which nobody
can undo.
*/
func (s *Service) PressAction(
	ctx context.Context,
	roomID, userID uuid.UUID,
	targetEventID, actionName string,
) (dbgen.Event, error) {
	// Membership rather than visibility: a public channel is readable by
	// everybody on the server, and answering a question in one is taking part.
	room, err := s.RequireMembership(ctx, roomID, userID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	target, err := s.store.GetEventByID(ctx, targetEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		// As missing, matching how a reaction reports one: which messages
		// exist is not something to confirm to somebody who guessed.
		return dbgen.Event{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read the message: %w", err)
	}
	if target.RoomID != roomID {
		// Named as missing rather than as belonging elsewhere: which channel a
		// message is in is not something to confirm to somebody who guessed.
		return dbgen.Event{}, ErrRoomNotFound
	}

	offered, err := actionNamed(target.Content, actionName)
	if err != nil {
		return dbgen.Event{}, err
	}
	if err := offered.Pressable(time.Now()); err != nil {
		return dbgen.Event{}, ErrActionExpired
	}

	/*
		A single-use action, checked before the write.

		This check has a window — two presses can both find nothing and both
		proceed — and the unique constraint below does not close it, because
		that constraint is per person and this rule is per button. What closes
		it is that the loser of the race is refused by the count on its retry
		and the winner's press is the one in the log; for a button offered once
		to a room of people, a rare double delivery is a cost worth naming
		rather than a transaction worth holding open across an append.
	*/
	if offered.Once {
		pressed, err := s.store.CountActionPresses(ctx, dbgen.CountActionPressesParams{
			EventID: targetEventID,
			Action:  actionName,
		})
		if err != nil {
			return dbgen.Event{}, fmt.Errorf("count presses: %w", err)
		}
		if pressed > 0 {
			return dbgen.Event{}, ErrAlreadyPressed
		}
	}

	presser, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read who pressed: %w", err)
	}

	/*
		The press itself, which is what makes "once" a database rule.

		No row means the conflict fired: this person has pressed this button
		before. Repeats are refused rather than silently succeeding, because a
		program receiving a second event for one decision cannot tell it from
		somebody changing their mind.
	*/
	_, err = s.store.RecordActionPress(ctx, dbgen.RecordActionPressParams{
		ID:          uuid.New(),
		EventID:     targetEventID,
		RoomID:      roomID,
		Action:      actionName,
		UserID:      userID,
		UserAddress: s.UserAddress(presser.Username),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, ErrAlreadyPressed
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("record the press: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		// What was pressed, and on which message. The program matched on the
		// name it chose rather than on a label anybody may translate.
		"action":  actionName,
		"pressed": targetEventID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode the press: %w", err)
	}

	return s.append(ctx, room, presser, events.TypeActionPressed, content, "")
}

/*
actionNamed finds one button among a message's offered actions.

A press naming something the message does not offer is refused. The alternative
— accepting it and delivering it — would let anybody send a program any action
name they liked, which is the same as letting them call its handlers directly.
*/
func actionNamed(content []byte, name string) (actions.Action, error) {
	var carried struct {
		Actions []actions.Action `json:"actions"`
	}
	if err := json.Unmarshal(content, &carried); err != nil {
		return actions.Action{}, ErrNoSuchAction
	}

	for _, action := range carried.Actions {
		if action.Name == name {
			return action, nil
		}
	}
	return actions.Action{}, ErrNoSuchAction
}
