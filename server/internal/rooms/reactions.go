package rooms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// ErrNotAnEmoji is returned when a reaction is not one.
var ErrNotAnEmoji = errors.New("a reaction must be an emoji")

/*
maxEmojiRunes bounds a reaction.

A single emoji can be several code points — a flag is two, a family with skin
tones can be a dozen joined by zero-width joiners — so this is generous. It
exists to stop the column being used as a text field, not to be exact about
what a grapheme is.
*/
const maxEmojiRunes = 24

// Reaction is one person reacting to a message.
type Reaction struct {
	Emoji string
	// Who reacted, as an address, so a client can name them and a federated
	// reaction from another server reads the same way.
	Users []string
	// Mine says whether the person asking is among them, which is what lets a
	// button show its own state.
	Mine bool
}

/*
validEmoji reports whether a string is plausibly a single emoji.

Deliberately a shape check rather than a lookup against the Unicode emoji
tables. Those change with every Unicode release, so a server that validated
against a fixed copy would start rejecting new emoji the moment a client
shipped support for them — which is a worse failure than accepting a symbol
that is not strictly an emoji.

What it does refuse is the thing that matters: letters, digits and whitespace,
which is what a reaction field gets abused with when it accepts free text.
*/
func validEmoji(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}

	count := 0
	for _, r := range value {
		count++
		if count > maxEmojiRunes {
			return false
		}

		// Letters, digits and spaces would make this a text field.
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return false
		}
		if unicode.IsControl(r) && r != '‍' && r != '️' {
			return false
		}
	}

	return true
}

/*
React adds a reaction to a message.

Membership is required rather than mere visibility: a public room can be read
without joining, but reacting is participation, and somebody who has not joined
should not appear in a room's conversation.
*/
func (s *Service) React(
	ctx context.Context,
	roomID, userID uuid.UUID,
	eventID, emoji string,
) error {
	emoji = strings.TrimSpace(emoji)
	if !validEmoji(emoji) {
		return ErrNotAnEmoji
	}

	if _, err := s.RequireMembership(ctx, roomID, userID); err != nil {
		return err
	}

	// The message has to be in this room. Without this check an identifier
	// from a conversation the person cannot see could be reacted to, which
	// would confirm that it exists.
	if _, err := s.EventInRoom(ctx, roomID, eventID); err != nil {
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("read reactor: %w", err)
	}

	return s.store.AddReaction(ctx, dbgen.AddReactionParams{
		ID:          uuid.New(),
		EventID:     eventID,
		RoomID:      roomID,
		UserID:      userID,
		UserAddress: s.UserAddress(user.Username),
		Emoji:       emoji,
	})
}

// Unreact removes one of somebody's own reactions.
//
// Scoped to the person, so this can only ever remove their own: a reaction is
// somebody's statement, and taking it back is theirs alone.
func (s *Service) Unreact(
	ctx context.Context,
	roomID, userID uuid.UUID,
	eventID, emoji string,
) error {
	if _, err := s.RequireMembership(ctx, roomID, userID); err != nil {
		return err
	}

	return s.store.RemoveReaction(ctx, dbgen.RemoveReactionParams{
		EventID: eventID,
		UserID:  userID,
		Emoji:   emoji,
	})
}

/*
ReactionsFor collects the reactions on a set of messages.

Grouped by emoji rather than returned as rows, because that is how they are
shown: one button per emoji with a count, not one per person. The reader's own
reactions are marked so a button can show that it is already pressed.
*/
func (s *Service) ReactionsFor(
	ctx context.Context,
	eventIDs []string,
	readerID uuid.UUID,
) (map[string][]Reaction, error) {
	if len(eventIDs) == 0 {
		return map[string][]Reaction{}, nil
	}

	rows, err := s.store.ListReactionsForEvents(ctx, eventIDs)
	if err != nil {
		return nil, fmt.Errorf("read reactions: %w", err)
	}

	// Grouped in insertion order, so the emoji somebody used first appears
	// first and the row does not reshuffle as reactions arrive.
	byEvent := make(map[string][]Reaction, len(eventIDs))
	index := make(map[string]map[string]int, len(eventIDs))

	for _, row := range rows {
		positions, seen := index[row.EventID]
		if !seen {
			positions = map[string]int{}
			index[row.EventID] = positions
		}

		at, exists := positions[row.Emoji]
		if !exists {
			byEvent[row.EventID] = append(byEvent[row.EventID], Reaction{Emoji: row.Emoji})
			at = len(byEvent[row.EventID]) - 1
			positions[row.Emoji] = at
		}

		reaction := &byEvent[row.EventID][at]
		reaction.Users = append(reaction.Users, row.UserAddress)
		if row.UserID == readerID {
			reaction.Mine = true
		}
	}

	return byEvent, nil
}

/*
EventInRoom reads a message, refusing one that belongs to another room.

Reported as missing rather than as forbidden, deliberately. Telling somebody
that a message exists but is not theirs to touch confirms it exists, which is
what an identifier taken from somewhere else would be probing for.

Shared by reactions, pins and reports, which all take an event identifier from
a client and must not let one from another conversation through.
*/
func (s *Service) EventInRoom(
	ctx context.Context,
	roomID uuid.UUID,
	eventID string,
) (dbgen.Event, error) {
	event, err := s.store.GetEventByID(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read event: %w", err)
	}
	if event.RoomID != roomID {
		return dbgen.Event{}, ErrRoomNotFound
	}

	return event, nil
}

// ValidEmojiForTest exposes the shape check to the package's tests.
//
// Exported for testing only: the rule is worth testing directly, because what
// it refuses is the whole reason the field cannot be used as free text.
func ValidEmojiForTest(value string) bool { return validEmoji(value) }
