package shell

import "testing"

/*
What closing the window does, and when it asks.

Getting this wrong loses somebody's work in one of two ways: a close that
quits when they meant to tuck it away, or a hide that leaves an application
running with no way to see or stop it.
*/

/*
Before anybody has chosen, closing asks and the window stays.

Guessing loses one thing or the other, and both are somebody's. The interface
asks; until it answers, nothing is destroyed.
*/
func TestBeforeAnybodyChoosesTheWindowStays(t *testing.T) {
	closing := &Closing{hasTray: true}

	if closing.Behaviour() != CloseAsks {
		t.Errorf("a fresh application starts at %q", closing.Behaviour())
	}
}

func TestAChoiceIsRemembered(t *testing.T) {
	closing := &Closing{hasTray: true}

	closing.SetBehaviour(CloseToTray)
	if closing.Behaviour() != CloseToTray {
		t.Errorf("after choosing the tray it is %q", closing.Behaviour())
	}

	closing.SetBehaviour(CloseQuits)
	if closing.Behaviour() != CloseQuits {
		t.Errorf("after changing to quit it is %q", closing.Behaviour())
	}
}

/*
Without a tray, closing quits whatever was chosen.

Hiding a window with no icon to bring it back leaves an application somebody
can neither see nor stop. A desktop environment with no tray, or one where the
icon failed to register, is a real machine — and a preference carried over
from one that had one must not strand somebody there.
*/
func TestWithoutATrayHidingIsRefused(t *testing.T) {
	if got := decide(CloseToTray, false); got != LetItClose {
		t.Errorf("with no tray, closing to tray gives %q — the window would vanish "+
			"with no way to bring it back", got)
	}
}

/* And with one, each choice does what it says. */
func TestEachChoiceDoesWhatItSays(t *testing.T) {
	for _, test := range []struct {
		behaviour CloseBehaviour
		hasTray   bool
		want      Decision
	}{
		{CloseToTray, true, HideIt},
		{CloseQuits, true, LetItClose},
		{CloseAsks, true, AskFirst},
		// No tray: every choice ends the application, because the alternative
		// is an application nobody can reach.
		{CloseToTray, false, LetItClose},
		{CloseQuits, false, LetItClose},
		{CloseAsks, false, LetItClose},
	} {
		if got := decide(test.behaviour, test.hasTray); got != test.want {
			t.Errorf("decide(%q, tray=%v) = %q, want %q",
				test.behaviour, test.hasTray, got, test.want)
		}
	}
}

/*
Without a tray, closing never stops to ask.

There is only one thing it can do, and a dialog offering a choice that does not
exist is worse than no dialog.
*/
func TestWithoutATrayNothingIsAsked(t *testing.T) {
	if got := decide(CloseAsks, false); got == AskFirst {
		t.Error("an application with no tray asked which of two things to do")
	}
}
