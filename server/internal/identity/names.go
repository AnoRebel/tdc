package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// MaxDisplayNameLength bounds a display name, in characters. Long enough for
// a full name in any script; short enough to fit a member list.
const MaxDisplayNameLength = 64

// ErrInvalidDisplayName reports a display name that cannot be accepted.
var ErrInvalidDisplayName = errors.New("invalid display name")

/*
ValidateDisplayName reports whether a display name may be used, once trimmed.

A display name is free text in any script — that is its whole point beside the
narrow username — so what is refused is only what can be used to deceive:

  - control characters, which have no business in a name and can break the
    lines it is drawn on;
  - invisible formatting characters (Unicode category Cf): right-to-left
    overrides that make a name read backwards, zero-width joiners and spaces
    that make two names look identical while comparing different;
  - a name that is, letters aside, one of the reserved usernames — "Admin",
    "S.y.s.t.e.m" — which would let anybody appear to speak for the server.

Empty is allowed: it means "show my username", which is what registration does
with no name given.
*/
func ValidateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)

	if len([]rune(trimmed)) > MaxDisplayNameLength {
		return fmt.Errorf("%w: at most %d characters", ErrInvalidDisplayName, MaxDisplayNameLength)
	}

	var letters strings.Builder
	previous := rune(0)
	for _, r := range trimmed {
		// The joiner inside an emoji sequence (woman, joiner, laptop) is a format character
		// too, and the one that is harmless: it is allowed after a symbol,
		// never between letters, where it would hide inside a word.
		joinsEmoji := r == '\u200d' && (unicode.IsSymbol(previous) || previous == '\ufe0f')
		if unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && !joinsEmoji) {
			return fmt.Errorf("%w: it contains a character that cannot be shown", ErrInvalidDisplayName)
		}
		previous = r
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters.WriteRune(unicode.ToLower(r))
		}
	}

	if reservedUsernames[letters.String()] {
		return fmt.Errorf("%w: that name is reserved", ErrInvalidDisplayName)
	}

	return nil
}

/*
HoldFormerNames tells the service how long a username somebody gave up stays
theirs.

A function rather than a value, because it is the operator's setting and can
change while the server runs. Until it is called, nothing is held — which is
only ever the case in a test that never renames anybody.
*/
func (s *Service) HoldFormerNames(period func(context.Context) time.Duration) {
	s.formerNameHold = period
}

/*
UsernameHeld reports whether somebody other than except gave this name up
within the holding period, and so may still come back to it.

Asked at registration as well as at a rename: an old name taken by a stranger
the day after it was given up would receive the mentions, the profile visits
and the trust meant for the person who had it.
*/
func (s *Service) UsernameHeld(ctx context.Context, folded string, except uuid.UUID) (bool, error) {
	if s.formerNameHold == nil {
		return false, nil
	}
	period := s.formerNameHold(ctx)
	if period <= 0 {
		return false, nil
	}

	held, err := s.store.UsernameHeldByOther(ctx, dbgen.UsernameHeldByOtherParams{
		Folded: folded, UserID: except, Since: time.Now().Add(-period),
	})
	if err != nil {
		return false, fmt.Errorf("check whether a name is held: %w", err)
	}
	return held, nil
}

/*
NameOf is what to call somebody wherever one person is named to another: their
display name, or their username when they have not set one.

The username stays the identifier — in mentions, addresses and links — but it
is not what people read as a name. A guest's username is generated, and a guest
always has a display name, so a guest is called by the name they gave.
*/
func NameOf(username, displayName string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name
	}
	return username
}
