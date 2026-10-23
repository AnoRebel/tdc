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
ErrStaleControls reports a press made through a view of a message whose
controls have since been replaced.

Refused rather than matched against the new controls by name: the person
answered the question they were shown, and if the program has asked a
different one since, counting their answer against it would record a decision
nobody made.
*/
var ErrStaleControls = errors.New("that message has changed since you saw it")

// ErrNotPermitted reports a press of a control restricted to people holding a
// permission the presser does not.
var ErrNotPermitted = errors.New("you may not answer that")

// Press is one activation of a control on a message.
type Press struct {
	RoomID uuid.UUID
	// UserID is who pressed, established from the authenticated request —
	// never from anything the client or the program says.
	UserID  uuid.UUID
	Message string
	Action  string
	// Value is what was chosen from a menu or typed into a field. Empty for a
	// button.
	Value string
	/*
		Version is which version of the message's controls the press was made
		against. Nil means whatever is current, which is what a client that
		predates versions sends — and is still refused a control that no
		longer exists.
	*/
	Version *int32
	/*
		Permits answers whether the presser holds a permission in this room.

		A function rather than a set of permissions, so this package does not
		learn how authorisation is resolved. Nil permits nothing, which is the
		safe reading of a caller that forgot to say.
	*/
	Permits func(permission string) bool
}

/*
PressAction records somebody pressing a button, and returns the event saying so.

The order is deliberate: the press is written first and the event appended
second. A press recorded without its event is a button that looks used and
tells nobody, which somebody can retry; an event announced without its press
recorded is a program told twice that a one-shot choice was made, which nobody
can undo.
*/
func (s *Service) PressAction(ctx context.Context, press Press) (dbgen.Event, error) {
	// Membership rather than visibility: a public channel is readable by
	// everybody on the server, and answering a question in one is taking part.
	room, err := s.RequireMembership(ctx, press.RoomID, press.UserID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	target, err := s.store.GetEventByID(ctx, press.Message)
	if errors.Is(err, pgx.ErrNoRows) {
		// As missing, matching how a reaction reports one: which messages
		// exist is not something to confirm to somebody who guessed.
		return dbgen.Event{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read the message: %w", err)
	}
	if target.RoomID != press.RoomID {
		// Named as missing rather than as belonging elsewhere: which channel a
		// message is in is not something to confirm to somebody who guessed.
		return dbgen.Event{}, ErrRoomNotFound
	}
	if target.RedactedAt != nil {
		// A question whose text is gone is not a question anybody can answer.
		return dbgen.Event{}, ErrNoSuchAction
	}

	current, err := s.Controls(ctx, target)
	if err != nil {
		return dbgen.Event{}, err
	}
	if press.Version != nil && *press.Version != current.Version {
		return dbgen.Event{}, ErrStaleControls
	}

	offered, err := actionNamed(current.Actions, press.Action)
	if err != nil {
		return dbgen.Event{}, err
	}
	if err := offered.Pressable(time.Now()); err != nil {
		return dbgen.Event{}, ErrActionExpired
	}

	/*
		Who may answer, decided about the person answering.

		Before the value is looked at and before anything is written: somebody
		without the permission learns nothing about what the control would
		have accepted, and the program is never told they tried.
	*/
	if offered.Requires != "" && (press.Permits == nil || !press.Permits(offered.Requires)) {
		return dbgen.Event{}, fmt.Errorf("%w: requires permission %q", ErrNotPermitted, offered.Requires)
	}
	if err := offered.Accepts(press.Value); err != nil {
		return dbgen.Event{}, err
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
			EventID: press.Message,
			Action:  press.Action,
			Version: current.Version,
		})
		if err != nil {
			return dbgen.Event{}, fmt.Errorf("count presses: %w", err)
		}
		if pressed > 0 {
			return dbgen.Event{}, ErrAlreadyPressed
		}
	}

	presser, err := s.store.GetUserByID(ctx, press.UserID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read who pressed: %w", err)
	}

	var value *string
	if press.Value != "" {
		value = &press.Value
	}

	/*
		The press itself, which is what makes "once" a database rule.

		No row means the conflict fired: this person has pressed this button,
		on this version of the controls, before. Repeats are refused rather
		than silently succeeding, because a program receiving a second event
		for one decision cannot tell it from somebody changing their mind.
	*/
	_, err = s.store.RecordActionPress(ctx, dbgen.RecordActionPressParams{
		ID:          uuid.New(),
		EventID:     press.Message,
		RoomID:      press.RoomID,
		Action:      press.Action,
		UserID:      press.UserID,
		UserAddress: s.UserAddress(presser.Username),
		Version:     current.Version,
		Value:       value,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, ErrAlreadyPressed
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("record the press: %w", err)
	}

	payload := map[string]any{
		// What was pressed, and on which message. The program matched on the
		// name it chose rather than on a label anybody may translate.
		"action":  press.Action,
		"pressed": press.Message,
		// Which version of the controls was answered, so a program that
		// updated the message can tell an answer to its old question from
		// one to its new.
		"version": current.Version,
	}
	if press.Value != "" {
		payload["value"] = press.Value
	}

	content, err := json.Marshal(payload)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode the press: %w", err)
	}

	return s.appendIn(ctx, room, presser, events.TypeActionPressed, content, "", target.ThreadRootID)
}

/*
actionNamed finds one control among a message's current ones.

A press naming something the message does not offer is refused. The alternative
— accepting it and delivering it — would let anybody send a program any action
name they liked, which is the same as letting them call its handlers directly.
*/
func actionNamed(offered []actions.Action, name string) (actions.Action, error) {
	for _, action := range offered {
		if action.Name == name {
			return action, nil
		}
	}
	return actions.Action{}, ErrNoSuchAction
}
