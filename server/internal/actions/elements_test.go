package actions_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/actions"
)

func options(count int) []actions.Option {
	listed := make([]actions.Option, 0, count)
	for i := range count {
		listed = append(listed, actions.Option{Value: fmt.Sprintf("v%d", i), Label: fmt.Sprintf("Choice %d", i)})
	}
	return listed
}

// A menu and a field are accepted alongside buttons in one message.
func TestAMenuAndAFieldAreAccepted(t *testing.T) {
	err := actions.Check([]actions.Action{
		{Name: "approve", Label: "Approve"},
		{Name: "environment", Label: "Environment", Kind: actions.KindSelect, Options: options(3)},
		{Name: "reason", Label: "Reason", Kind: actions.KindInput, Placeholder: "Why?", MaxLength: 120},
	})
	if err != nil {
		t.Fatalf("a message with a button, a menu and a field was refused: %v", err)
	}
}

/*
A menu carrying more options than anybody can use is refused at post time.

The limit is exactly the documented one: a menu at the limit is accepted, and
one past it is not, so the boundary is where the documentation says.
*/
func TestAMenuWithTooManyOptionsIsRefused(t *testing.T) {
	atLimit := []actions.Action{{Name: "pick", Label: "Pick", Kind: actions.KindSelect,
		Options: options(actions.MaxOptions)}}
	if err := actions.Check(atLimit); err != nil {
		t.Fatalf("a menu at the limit was refused: %v", err)
	}

	overLimit := []actions.Action{{Name: "pick", Label: "Pick", Kind: actions.KindSelect,
		Options: options(actions.MaxOptions + 1)}}
	err := actions.Check(overLimit)
	if !errors.Is(err, actions.ErrTooManyOptions) {
		t.Fatalf("a menu past the limit returned %v, want ErrTooManyOptions", err)
	}
	if !errors.Is(err, actions.ErrInvalidElement) {
		t.Error("the refusal does not say the control is invalid, so a caller cannot report it as one")
	}
}

// An input with no field name has nothing to report its value under.
func TestAnInputWithNoFieldNameIsRefused(t *testing.T) {
	err := actions.Check([]actions.Action{{Name: "  ", Label: "Reason", Kind: actions.KindInput}})
	if !errors.Is(err, actions.ErrNameRequired) {
		t.Fatalf("a field with no name returned %v, want ErrNameRequired", err)
	}
}

// The rest of what a menu or a field can get wrong, each named.
func TestMalformedElementsAreRefused(t *testing.T) {
	cases := map[string]struct {
		action actions.Action
		want   error
	}{
		"empty menu": {actions.Action{Name: "m", Label: "M", Kind: actions.KindSelect}, actions.ErrNoOptions},
		"option with no label": {actions.Action{Name: "m", Label: "M", Kind: actions.KindSelect,
			Options: []actions.Option{{Value: "a"}}}, actions.ErrOptionInvalid},
		"duplicate option": {actions.Action{Name: "m", Label: "M", Kind: actions.KindSelect,
			Options: []actions.Option{{Value: "a", Label: "A"}, {Value: "a", Label: "Also A"}}},
			actions.ErrDuplicateOption},
		"field too long": {actions.Action{Name: "f", Label: "F", Kind: actions.KindInput,
			MaxLength: actions.MaxInputLength + 1}, actions.ErrInputBounds},
		"placeholder too long": {actions.Action{Name: "f", Label: "F", Kind: actions.KindInput,
			Placeholder: strings.Repeat("x", actions.MaxPlaceholderLength+1)}, actions.ErrPlaceholderTooLong},
		"unknown kind": {actions.Action{Name: "d", Label: "D", Kind: "datepicker"}, actions.ErrUnknownKind},
		"menu that opens a link": {actions.Action{Name: "m", Label: "M", Kind: actions.KindSelect,
			Capability: actions.CapabilityOpen, URL: "https://example.org", Options: options(1)},
			actions.ErrOnlyButtons},
		"unknown requirement": {actions.Action{Name: "b", Label: "B", Requires: "channel.everything"},
			actions.ErrUnknownRequirement},
		"server-only requirement": {actions.Action{Name: "b", Label: "B", Requires: "server.administer"},
			actions.ErrUnknownRequirement},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			err := actions.Check([]actions.Action{test.action})
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}

// A press carries a value the control offered, and nothing else.
func TestAPressValueMustBeOneTheControlOffered(t *testing.T) {
	menu := actions.Action{Name: "m", Label: "M", Kind: actions.KindSelect, Options: options(2)}
	if err := menu.Accepts("v1"); err != nil {
		t.Errorf("a listed choice was refused: %v", err)
	}
	if err := menu.Accepts("v9"); !errors.Is(err, actions.ErrValueRefused) {
		t.Errorf("a choice the menu never offered returned %v", err)
	}

	field := actions.Action{Name: "f", Label: "F", Kind: actions.KindInput, MaxLength: 5}
	if err := field.Accepts("12345"); err != nil {
		t.Errorf("text at the field's limit was refused: %v", err)
	}
	if err := field.Accepts("123456"); !errors.Is(err, actions.ErrValueRefused) {
		t.Errorf("text past the field's limit returned %v", err)
	}
	if err := field.Accepts("   "); !errors.Is(err, actions.ErrValueRefused) {
		t.Errorf("an empty field returned %v", err)
	}

	button := actions.Action{Name: "b", Label: "B"}
	if err := button.Accepts("smuggled"); !errors.Is(err, actions.ErrValueRefused) {
		t.Errorf("a button press carrying a value returned %v", err)
	}
}
