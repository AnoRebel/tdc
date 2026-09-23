package actions

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/AnoRebel/tdc/server/internal/authz"
)

/*
Controls beyond a button: a menu of choices, and a short field to type into.

Both are validated where buttons are, by Check when the message is posted, and
for the same reason: a menu with nothing in it, or a field with no name to
report its value under, is a control that fails only once somebody has read the
message and tried to answer it.

A press of either carries a value — the option chosen, or the text typed —
which is checked here too, against what the message actually offered. A program
receives the value of an option it listed or text within the length it asked
for, and never anything else, because the alternative is every program having
to validate input that came through a control it thought it had constrained.
*/

// Kind names a sort of control.
type Kind string

const (
	// KindButton is the ordinary control, written as an empty kind.
	KindButton Kind = ""
	// KindSelect offers a list of choices, of which one is picked.
	KindSelect Kind = "select"
	// KindInput is a short line of text.
	KindInput Kind = "input"
)

// Kinds is every kind this server accepts, for a client deciding what it can
// render.
func Kinds() []Kind {
	return []Kind{"button", KindSelect, KindInput}
}

// Option is one choice in a menu.
type Option struct {
	// Value is what a press reports back, as Name is for a button.
	Value string `json:"value"`
	// Label is what a reader sees.
	Label string `json:"label"`
}

/*
MaxOptions bounds a menu.

Twenty-five, because a menu longer than that is a search, and a phone showing
it is a long scroll through a list somebody has to read end to end. It also
bounds the size of the message carrying it.
*/
const MaxOptions = 25

// MaxOptionValueLength bounds what a choice reports back.
const MaxOptionValueLength = 100

// MaxInputLength is the most a field may ask for, and what it allows when it
// states no bound of its own.
const MaxInputLength = 200

// MaxPlaceholderLength bounds the hint in an empty field.
const MaxPlaceholderLength = 80

// ErrInvalidElement is what every refusal of a menu or a field wraps, so a
// caller can report them as the caller's mistake without listing each.
var ErrInvalidElement = errors.New("that control is not one a message can carry")

var (
	// ErrUnknownKind reports a control this server does not know how to offer.
	ErrUnknownKind = fmt.Errorf("%w: unknown kind", ErrInvalidElement)
	// ErrNoOptions reports a menu with nothing to choose.
	ErrNoOptions = fmt.Errorf("%w: a menu needs at least one option", ErrInvalidElement)
	// ErrTooManyOptions reports a menu longer than anybody can use.
	ErrTooManyOptions = fmt.Errorf("%w: a menu offers too many options", ErrInvalidElement)
	// ErrOptionInvalid reports an option with no value or no label.
	ErrOptionInvalid = fmt.Errorf("%w: every option needs a value and a label", ErrInvalidElement)
	// ErrDuplicateOption reports two options a choice could not be told apart by.
	ErrDuplicateOption = fmt.Errorf("%w: two options share a value", ErrInvalidElement)
	// ErrInputBounds reports a field asking for more than a field allows.
	ErrInputBounds = fmt.Errorf("%w: an input field allows at most %d characters",
		ErrInvalidElement, MaxInputLength)
	// ErrPlaceholderTooLong reports a hint too long to fit in the field.
	ErrPlaceholderTooLong = fmt.Errorf("%w: that placeholder is too long", ErrInvalidElement)
	// ErrOnlyButtons reports a capability on something that is not a button.
	ErrOnlyButtons = fmt.Errorf("%w: only a button opens anything", ErrInvalidElement)
	// ErrUnknownRequirement reports a restriction naming no permission this
	// server has.
	ErrUnknownRequirement = fmt.Errorf("%w: requires names no permission a channel grants",
		ErrInvalidElement)
)

// checkElement validates what a menu or a field carries.
func checkElement(action Action) error {
	if action.Requires != "" {
		permission := authz.Permission(action.Requires)
		if _, known := authz.Lookup(permission); !known || !authz.GrantableAt(permission, authz.ScopeChannel) {
			return fmt.Errorf("%w: %q", ErrUnknownRequirement, action.Requires)
		}
	}

	switch action.Kind {
	case KindButton:
		if len(action.Options) > 0 {
			return fmt.Errorf("%w: a button offers no options", ErrInvalidElement)
		}
		return nil

	case KindSelect:
		if action.Capability != "" {
			return ErrOnlyButtons
		}
		return checkOptions(action.Options)

	case KindInput:
		if action.Capability != "" {
			return ErrOnlyButtons
		}
		if len(action.Options) > 0 {
			return fmt.Errorf("%w: a field offers no options", ErrInvalidElement)
		}
		if action.MaxLength < 0 || action.MaxLength > MaxInputLength {
			return ErrInputBounds
		}
		if utf8.RuneCountInString(action.Placeholder) > MaxPlaceholderLength {
			return ErrPlaceholderTooLong
		}
		return nil

	default:
		return fmt.Errorf("%w: %q", ErrUnknownKind, action.Kind)
	}
}

func checkOptions(options []Option) error {
	if len(options) == 0 {
		return ErrNoOptions
	}
	if len(options) > MaxOptions {
		return fmt.Errorf("%w: at most %d", ErrTooManyOptions, MaxOptions)
	}

	seen := make(map[string]bool, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		label := strings.TrimSpace(option.Label)
		if value == "" || label == "" {
			return ErrOptionInvalid
		}
		if utf8.RuneCountInString(value) > MaxOptionValueLength ||
			utf8.RuneCountInString(label) > MaxLabelLength {
			return fmt.Errorf("%w: %q is too long", ErrOptionInvalid, value)
		}
		if seen[value] {
			return fmt.Errorf("%w: %q", ErrDuplicateOption, value)
		}
		seen[value] = true
	}
	return nil
}

// ErrValueRefused reports a press carrying something the control did not
// offer — a choice not on the menu, or text the field does not accept.
var ErrValueRefused = errors.New("that is not something this control accepts")

/*
Accepts checks the value a press carries against what the control offered.

A button carries none, and a value sent with one is refused rather than passed
along: a program that never asked for text should not find some in the press.
*/
func (a Action) Accepts(value string) error {
	switch a.Kind {
	case KindSelect:
		for _, option := range a.Options {
			if option.Value == value {
				return nil
			}
		}
		return fmt.Errorf("%w: %q is not one of the choices", ErrValueRefused, value)

	case KindInput:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("%w: the field is empty", ErrValueRefused)
		}
		limit := a.MaxLength
		if limit == 0 {
			limit = MaxInputLength
		}
		if utf8.RuneCountInString(trimmed) > limit {
			return fmt.Errorf("%w: at most %d characters", ErrValueRefused, limit)
		}
		return nil

	default:
		if value != "" {
			return fmt.Errorf("%w: a button carries no value", ErrValueRefused)
		}
		return nil
	}
}
