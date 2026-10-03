/*
Package actions describes the buttons a message may carry, and what pressing
one is allowed to do.

A program posts a message offering choices; somebody presses one; the program
finds out and decides what happens. That is the whole shape, and the part worth
being careful about is the boundary between what the *server* does when a
button is pressed and what the *client* does.

Almost every action emits an event to whoever posted the message and nothing
else. The program decides what a press means, because it is the only thing that
knows. The exception is a small, fixed set of things a client can do by itself —
opening a link is the only one today — and those are named here rather than
inferred, so a message cannot ask a client to do something nobody agreed to.

An action naming a capability this server does not declare is refused when the
message is posted, not when somebody presses it. A button that only fails on
being pressed is a button somebody has already decided to trust.
*/
package actions

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrTooMany reports a message carrying more buttons than anybody can use.
var ErrTooMany = errors.New("a message carries too many actions")

// ErrLabelRequired reports a button with nothing written on it.
var ErrLabelRequired = errors.New("an action needs a label")

// ErrLabelTooLong reports a label that would not fit on a button.
var ErrLabelTooLong = errors.New("that label is too long for a button")

// ErrNameRequired reports an action the sender could not identify a press of.
var ErrNameRequired = errors.New("an action needs a name its sender will recognise")

// ErrDuplicateName reports two actions a press could not be told apart by.
var ErrDuplicateName = errors.New("two actions share a name, so a press could not say which")

// ErrUnknownCapability reports an action asking a client to do something this
// server does not declare.
var ErrUnknownCapability = errors.New("that is not something a client will do")

// ErrURLRequired reports an open action with nowhere to go.
var ErrURLRequired = errors.New("an action that opens a link needs one")

// ErrURLNotAllowed reports a link a client will not open.
var ErrURLNotAllowed = errors.New("a client will only open http and https links")

/*
MaxActions bounds how many buttons one message may carry.

Five, because a row of buttons is a question and a question with more than a
handful of answers is a list — which is a different control, and one the
client would have to render differently. It also bounds the work a single press
can be turned into.
*/
const MaxActions = 5

// MaxLabelLength bounds what is written on a button.
const MaxLabelLength = 40

// MaxNameLength bounds the identifier a press carries back.
const MaxNameLength = 64

/*
Capability names something a client will do by itself, without asking the
program that posted the message.

The set is fixed and small on purpose. Every addition is a thing every client
must then implement and a thing a message can ask any reader's client to do, so
it is a decision rather than a configuration.
*/
type Capability string

const (
	/*
		CapabilityOpen opens a link.

		The only one today. It earns its place because the alternative — a
		round trip to the program, which replies "now open this" — is slower
		for no benefit and gives the program a way to learn that somebody
		pressed a button whose whole purpose was to go somewhere public.
	*/
	CapabilityOpen Capability = "open"
)

// Capabilities is every capability this server declares.
func Capabilities() []Capability {
	return []Capability{CapabilityOpen}
}

/*
Action is one button on a message.

Name is what a press reports back, so the program can tell which button it was
without matching on a label somebody may translate. Label is what a reader
sees.
*/
type Action struct {
	Name  string `json:"name"`
	Label string `json:"label"`

	/*
		Capability names what the client does by itself, when anything.

		Empty is the ordinary case and means the press becomes an event to
		whoever posted the message. That default matters: an action that did
		nothing unless it named a capability would make the common case the
		one requiring ceremony.
	*/
	Capability Capability `json:"capability,omitempty"`

	// URL is where CapabilityOpen goes. Meaningless otherwise.
	URL string `json:"url,omitempty"`

	/*
		Once marks an action that may be pressed one time in total, by
		anybody.

		Enforced by the server so a program does not have to defend against
		replay itself — which is the kind of thing every integration would
		otherwise get slightly wrong.
	*/
	Once bool `json:"once,omitempty"`

	// Expires marks when the button stops working. Absent means it does not.
	Expires *time.Time `json:"expires,omitempty"`

	/*
		Kind says what sort of control this is: a button when empty, or a
		selection menu or a short input field.

		Empty rather than "button" for the ordinary case, so a message posted
		before menus existed is exactly the message it was — and so is one
		posted by a program that has never heard of them.
	*/
	Kind Kind `json:"kind,omitempty"`

	// Options are what a menu offers. Meaningless for any other kind.
	Options []Option `json:"options,omitempty"`

	// Placeholder is a hint shown in an empty input field.
	Placeholder string `json:"placeholder,omitempty"`

	// MaxLength bounds what may be typed into an input field. Zero is the
	// largest a field allows.
	MaxLength int `json:"maxLength,omitempty"`

	/*
		Requires restricts who may activate this control to people holding a
		permission, in the channel the message is in.

		Checked against the person pressing, never against whoever posted the
		message: a program's authority is not something a reader borrows by
		clicking its button.
	*/
	Requires string `json:"requires,omitempty"`
}

/*
Check validates the actions a message carries, before it is posted.

Refusing here rather than at the press is the point. A message that reached a
channel and then failed when anybody touched it has already been read, already
been trusted, and already told everybody that somebody offered them a choice.
*/
func Check(actions []Action) error {
	if len(actions) > MaxActions {
		return fmt.Errorf("%w: at most %d", ErrTooMany, MaxActions)
	}

	seen := make(map[string]bool, len(actions))
	for _, action := range actions {
		if err := checkOne(action); err != nil {
			return err
		}
		if seen[action.Name] {
			return fmt.Errorf("%w: %q", ErrDuplicateName, action.Name)
		}
		seen[action.Name] = true
	}
	return nil
}

func checkOne(action Action) error {
	name := strings.TrimSpace(action.Name)
	if name == "" {
		return ErrNameRequired
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return fmt.Errorf("%w: %q", ErrNameRequired, name)
	}

	label := strings.TrimSpace(action.Label)
	if label == "" {
		return ErrLabelRequired
	}
	if utf8.RuneCountInString(label) > MaxLabelLength {
		return fmt.Errorf("%w: at most %d characters", ErrLabelTooLong, MaxLabelLength)
	}

	// A menu or a field is checked for what it carries; a capability belongs
	// to buttons alone, because only a button does something by being pressed.
	if err := checkElement(action); err != nil {
		return err
	}
	if action.Kind != KindButton {
		return nil
	}

	switch action.Capability {
	case "":
		// The ordinary case: a press becomes an event to the sender.
		return nil
	case CapabilityOpen:
		return checkURL(action.URL)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownCapability, action.Capability)
	}
}

/*
checkURL refuses a link a client should not follow.

http and https only. A javascript: or data: link in a message anybody can post
is a way to run something in every reader's client, and a file: link is a way
to ask their machine about itself — neither is a thing a button in a chat
message gets to do.
*/
func checkURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return ErrURLRequired
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrURLNotAllowed, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: %q", ErrURLNotAllowed, parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: it names no host", ErrURLNotAllowed)
	}
	return nil
}

/*
Pressable reports whether an action can still be pressed, and why not.

Expiry is checked here; single use is checked against what has already been
recorded, which this package does not hold.
*/
func (a Action) Pressable(now time.Time) error {
	if a.Expires != nil && !now.Before(*a.Expires) {
		return ErrExpired
	}
	return nil
}

// ErrExpired reports a press of a button whose time has passed.
var ErrExpired = errors.New("that is no longer offered")
