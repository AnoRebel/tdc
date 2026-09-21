package actions_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/actions"
)

/*
What a message may offer, and what it may not.

The refusals matter more than the acceptances here. A button is a thing a
reader trusts because it is in a message they are reading, so the question
every one of these asks is what somebody could put in front of a reader that
they should not be able to.
*/

// The ordinary case: a labelled button whose press goes to the sender.
func TestAnOrdinaryActionIsAccepted(t *testing.T) {
	err := actions.Check([]actions.Action{
		{Name: "approve", Label: "Approve"},
		{Name: "reject", Label: "Reject"},
	})
	if err != nil {
		t.Errorf("two plain buttons were refused: %v", err)
	}
}

/*
A capability this server does not declare is refused when the message is
posted.

The whole point of declaring a fixed set. A message asking a client to do
something nobody agreed to must not reach a channel and then fail on being
pressed — by then it has been read, and somebody has already decided the button
was worth trusting.
*/
func TestAnUndeclaredCapabilityIsRefused(t *testing.T) {
	err := actions.Check([]actions.Action{
		{Name: "run", Label: "Run it", Capability: "execute"},
	})
	if !errors.Is(err, actions.ErrUnknownCapability) {
		t.Errorf("an action asking clients to execute something gave %v", err)
	}
}

// And the one that is declared works, with somewhere to go.
func TestOpeningALinkIsDeclared(t *testing.T) {
	err := actions.Check([]actions.Action{
		{Name: "docs", Label: "Read the docs", Capability: actions.CapabilityOpen,
			URL: "https://example.test/docs"},
	})
	if err != nil {
		t.Errorf("opening an https link was refused: %v", err)
	}
}

/*
A link a client will not follow.

javascript: in a message anybody can post is a way to run something in every
reader's client; file: is a way to ask their machine about itself. Neither is
a thing a button in a chat message gets to do.
*/
func TestOnlyHTTPLinksAreOpened(t *testing.T) {
	/*
		Two guards refuse these, and the split is worth knowing. A scheme that
		carries no host — javascript:, data:, file: — is caught by the host
		check even if the scheme check is removed, and one that does carry a
		host is caught only by the scheme check. Sabotaging either alone
		therefore leaves the other covering part of the list, which is defence
		in depth working and a test that cannot tell them apart.

		So each is named with the guard that is its last line, and both are
		exercised.
	*/
	for _, probe := range []struct {
		link, refusedBy string
	}{
		{"javascript:alert(1)", "no host, and not an allowed scheme"},
		{"data:text/html,<script>alert(1)</script>", "no host, and not an allowed scheme"},
		{"file:///etc/passwd", "no host, and not an allowed scheme"},
		{"ftp://example.test/thing", "an allowed host, so only the scheme refuses it"},
		{"https:///nowhere", "an allowed scheme, so only the missing host refuses it"},
	} {
		err := actions.Check([]actions.Action{
			{Name: "go", Label: "Go", Capability: actions.CapabilityOpen, URL: probe.link},
		})
		if !errors.Is(err, actions.ErrURLNotAllowed) {
			t.Errorf("a client was asked to open %q (%s) and the message was accepted: %v",
				probe.link, probe.refusedBy, err)
		}
	}
}

// An open action with nowhere to go is refused rather than rendered inert.
func TestOpeningNothingIsRefused(t *testing.T) {
	err := actions.Check([]actions.Action{
		{Name: "go", Label: "Go", Capability: actions.CapabilityOpen},
	})
	if !errors.Is(err, actions.ErrURLRequired) {
		t.Errorf("an open action with no link gave %v", err)
	}
}

/*
Two actions a press could not be told apart by.

The name is what comes back when somebody presses. Two buttons sharing one
means the program cannot know which was pressed, and the failure would look
like the reader having chosen something they did not.
*/
func TestTwoActionsCannotShareAName(t *testing.T) {
	err := actions.Check([]actions.Action{
		{Name: "choose", Label: "Yes"},
		{Name: "choose", Label: "No"},
	})
	if !errors.Is(err, actions.ErrDuplicateName) {
		t.Errorf("two buttons named the same were accepted: %v", err)
	}
}

// A button needs something written on it, and a name to report.
func TestAButtonNeedsALabelAndAName(t *testing.T) {
	if err := actions.Check([]actions.Action{{Name: "ok"}}); !errors.Is(err, actions.ErrLabelRequired) {
		t.Errorf("a button with no label gave %v", err)
	}
	if err := actions.Check([]actions.Action{{Label: "OK"}}); !errors.Is(err, actions.ErrNameRequired) {
		t.Errorf("a button with no name gave %v", err)
	}
}

// A label that would not fit on a button is refused rather than truncated,
// because truncation changes what somebody thinks they are agreeing to.
func TestALabelThatWouldNotFitIsRefused(t *testing.T) {
	err := actions.Check([]actions.Action{
		{Name: "long", Label: strings.Repeat("a", actions.MaxLabelLength+1)},
	})
	if !errors.Is(err, actions.ErrLabelTooLong) {
		t.Errorf("an oversized label gave %v", err)
	}
}

// A row of buttons is a question; too many is a list, which is a different
// control the client would have to render differently.
func TestTooManyActionsAreRefused(t *testing.T) {
	many := make([]actions.Action, actions.MaxActions+1)
	for i := range many {
		many[i] = actions.Action{Name: string(rune('a' + i)), Label: "Choice"}
	}
	if err := actions.Check(many); !errors.Is(err, actions.ErrTooMany) {
		t.Errorf("%d actions gave %v", len(many), err)
	}
}

// An expiry is enforced, so a program does not have to police its own replay.
func TestAnExpiredActionIsNotPressable(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	action := actions.Action{Name: "vote", Label: "Vote", Expires: &past}

	if err := action.Pressable(time.Now()); !errors.Is(err, actions.ErrExpired) {
		t.Errorf("a button that expired a minute ago is still pressable: %v", err)
	}

	future := time.Now().Add(time.Hour)
	fresh := actions.Action{Name: "vote", Label: "Vote", Expires: &future}
	if err := fresh.Pressable(time.Now()); err != nil {
		t.Errorf("a button that expires in an hour was refused: %v", err)
	}
}

// A message carrying nothing is the ordinary message, and must stay cheap.
func TestNoActionsIsFine(t *testing.T) {
	if err := actions.Check(nil); err != nil {
		t.Errorf("a message with no actions was refused: %v", err)
	}
}
