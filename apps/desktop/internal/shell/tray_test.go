package shell

import (
	"strings"
	"testing"
	"time"
)

/*
What the tray says, without a tray.

The icon itself needs a desktop session, which CI has none of — so what is
checked here is the part that decides what somebody reads: the count on the
badge, how long a mute has left, and what "until tomorrow" means.
*/

/*
A count past ninety-nine stops being a number and becomes "a lot".

The exact figure means nothing at that point and crowds whatever it sits
beside, which on a panel is everything.
*/
func TestTheBadgeStaysShort(t *testing.T) {
	for _, test := range []struct {
		count int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{99, "99"},
		{100, "99+"},
		{4821, "99+"},
	} {
		if got := badge(test.count); got != test.want {
			t.Errorf("badge(%d) = %q, want %q", test.count, got, test.want)
		}
	}
}

// A tooltip reads as a sentence, so the count is written in words around it.
func TestTheTooltipCountsInWords(t *testing.T) {
	if got := messages(1); got != "1 message" {
		t.Errorf("one message reads as %q", got)
	}
	if got := messages(4); got != "4 messages" {
		t.Errorf("four messages read as %q", got)
	}
}

/*
How much of a mute is left, as somebody reading a menu wants it.

Rounded to the minute: whether it is nearly over is the question, not whether
fifty-eight seconds remain.
*/
func TestRemainingIsRoundedForReading(t *testing.T) {
	for _, test := range []struct {
		left time.Duration
		want string
	}{
		{30 * time.Second, "a minute"},
		{14 * time.Minute, "14 minutes"},
		{time.Hour, "1 hour"},
		{3 * time.Hour, "3 hours"},
		// Whole hours, rounded down. An hour and a half reads as "1 hour"
		// rather than "2 hours", which would say there is more time left than
		// there is — the wrong direction to be wrong in for something somebody
		// is waiting to end.
		{90 * time.Minute, "1 hour"},
		{119 * time.Minute, "1 hour"},
	} {
		if got := remaining(test.left); got != test.want {
			t.Errorf("remaining(%s) = %q, want %q", test.left, got, test.want)
		}
	}
}

/*
"Until tomorrow" means the start of the next day.

Somebody muting an application at nine in the evening means until they are back
at it, not for twenty-four hours — which would silence it through most of the
next working day.
*/
func TestUntilTomorrowIsTheStartOfTheNextDay(t *testing.T) {
	minutes := minutesUntilTomorrow()

	if minutes <= 0 {
		t.Fatalf("until tomorrow is %d minutes away", minutes)
	}
	// A whole day at most, and by definition less than one.
	if minutes > 24*60 {
		t.Errorf("until tomorrow is %d minutes, which is more than a day", minutes)
	}

	/*
		Local midnight, not a fixed span and not UTC midnight.

		Truncate works in UTC, so an expectation built with it is wrong by the
		offset — which is how this test first failed while the code was right,
		in a zone three hours from UTC.
	*/
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, 1)
	expected := time.Until(midnight)

	if diff := time.Duration(minutes)*time.Minute - expected; diff > time.Minute || diff < -time.Minute {
		t.Errorf("until tomorrow is %d minutes; local midnight is %s away", minutes, expected)
	}
}

/*
Every event the tray sends is one the frontend knows.

The same drift the menu events have a test for, and for the same reason: a
rename that misses one side leaves a menu item that silently does nothing.
*/
func TestTheFrontendKnowsEveryTrayEvent(t *testing.T) {
	generated := readGenerated(t)

	for _, event := range []string{
		EventMuteFor,
		EventUnmute,
		EventAddServer,
		EventNewChannelFromTray,
		EventOpenRoom,
		EventQuitRequested,
	} {
		if !strings.Contains(generated, `"`+event+`"`) {
			t.Errorf("%q is sent by the tray and unknown to the frontend — "+
				"the item would do nothing; run `go generate ./internal/shell`", event)
		}
	}
}

/*
On Linux the tray is named, always, including with nothing waiting.

The label there is the StatusNotifierItem's Title, and what panels show on
hover, because Wails leaves the tooltip unimplemented on Linux. An empty label
hovered as nothing, and one never set before Run hovered as "Wails" — which is
what shipped.
*/
func TestTheLinuxTrayAlwaysCarriesItsName(t *testing.T) {
	for _, count := range []int{0, 1, 7, 250} {
		label, _ := trayText("linux", count)
		if !strings.HasPrefix(label, TrayName) {
			t.Errorf("with %d waiting the Linux label is %q, which does not name the application", count, label)
		}
	}

	label, _ := trayText("linux", 3)
	if !strings.Contains(label, "3 messages") {
		t.Errorf("the Linux label %q does not say how many are waiting", label)
	}
}

// On macOS the label sits beside the icon, so it is the bare count or nothing:
// "TDC" beside the TDC icon says the same thing twice.
func TestTheMacTrayLabelIsTheCountAlone(t *testing.T) {
	if label, _ := trayText("darwin", 0); label != "" {
		t.Errorf("with nothing waiting the menu bar reads %q, want nothing", label)
	}
	if label, _ := trayText("darwin", 120); label != "99+" {
		t.Errorf("the menu bar reads %q, want 99+", label)
	}
}

// Every platform's tooltip names the application, whatever the count.
func TestEveryTooltipNamesTheApplication(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, count := range []int{0, 2} {
			if _, tooltip := trayText(goos, count); !strings.HasPrefix(tooltip, TrayName) {
				t.Errorf("%s with %d waiting: tooltip %q does not name the application", goos, count, tooltip)
			}
		}
	}
}
