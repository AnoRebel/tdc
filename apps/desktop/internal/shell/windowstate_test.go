package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

/*
Putting the window back where it was, without losing it.

The failure worth preventing: a position remembered from a monitor that is no
longer connected, restored anyway, leaving a window nobody can see or reach. A
laptop that was docked yesterday and is on a train today is the ordinary case,
not an exotic one.
*/

// One monitor, as a docked laptop and its external screen would report.
var (
	laptop   = application.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}
	external = application.Rect{X: 1920, Y: 0, Width: 2560, Height: 1440}
)

func TestAWindowOnAConnectedScreenIsRestored(t *testing.T) {
	placement := Placement{X: 100, Y: 100, Width: 900, Height: 700}

	if !placement.OnScreen([]application.Rect{laptop}) {
		t.Error("a window in the middle of the only screen was judged unreachable")
	}
}

/*
The case this exists for: the external monitor is gone.

Everything about the placement is still valid — it is a real size at real
coordinates. It is simply nowhere somebody can see.
*/
func TestAWindowOnADisconnectedScreenIsNotRestored(t *testing.T) {
	// Where it sat while docked.
	placement := Placement{X: 2400, Y: 300, Width: 900, Height: 700}

	if placement.OnScreen([]application.Rect{laptop, external}) == false {
		t.Fatal("the placement was unreachable even with the monitor connected")
	}

	if placement.OnScreen([]application.Rect{laptop}) {
		t.Error("a window on a disconnected monitor was judged reachable; " +
			"it would open where nobody can see it")
	}
}

/*
A sliver at the edge is not good enough.

A window whose left few pixels overlap a screen is one somebody cannot
realistically grab, and treating it as fine is the same failure with extra
steps.
*/
func TestASliverAtTheEdgeIsNotEnough(t *testing.T) {
	// Ten pixels of a 900-wide window on screen.
	placement := Placement{X: 1910, Y: 300, Width: 900, Height: 700}

	if placement.OnScreen([]application.Rect{laptop}) {
		t.Error("a window with a sliver on screen was judged reachable")
	}
}

/*
And what somebody grabs is the top of a window.

A window pushed up so that only its lower half shows has plenty of area on
screen and no title bar to take hold of. Judging by total overlap accepts it;
judging by the strip somebody actually grabs does not — which is the whole
reason this looks at the top rather than the whole.

The offset matters: -400 of a 700-high window leaves 300 pixels visible, so a
total-overlap test would see a third of the window and be satisfied.
*/
func TestAWindowPushedOffTheTopIsNotReachable(t *testing.T) {
	placement := Placement{X: 100, Y: -400, Width: 900, Height: 700}

	if placement.OnScreen([]application.Rect{laptop}) {
		t.Error("a window with only its lower half visible was judged reachable; " +
			"there is no title bar on screen to drag it back by")
	}
}

/* No screens at all — a session with no display. Nothing is reachable. */
func TestWithNoScreensNothingIsReachable(t *testing.T) {
	placement := Placement{X: 100, Y: 100, Width: 900, Height: 700}

	if placement.OnScreen(nil) {
		t.Error("a window was judged reachable with no screens connected")
	}
}

/*
A placement with no size is not one.

What an unwritten or half-written file gives, and restoring to it would produce
a window with no area at all.
*/
func TestAPlacementWithNoSizeIsRefused(t *testing.T) {
	for _, placement := range []Placement{
		{},
		{X: 100, Y: 100},
		{X: 100, Y: 100, Width: 900},
		{X: 100, Y: 100, Height: 700},
	} {
		if placement.Valid() {
			t.Errorf("%+v was accepted as somewhere to put a window", placement)
		}
	}
}

func TestAPlacementSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")

	writePlacement(path, Placement{X: 40, Y: 60, Width: 1100, Height: 720, Maximised: true})

	saved, ok := readPlacement(path)
	if !ok {
		t.Fatal("nothing came back")
	}
	if saved.X != 40 || saved.Width != 1100 || !saved.Maximised {
		t.Errorf("what came back is %+v", saved)
	}
}

func TestAnUnreadablePlacementIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	if _, ok := readPlacement(path); ok {
		t.Error("a broken file was read as a placement")
	}
}

/*
Opening at login and opening hidden are separate choices.

Somebody may want it running quietly from login, or want a window when they
open it themselves and nothing at login at all.
*/
func TestStartupChoicesAreIndependent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.json")

	if err := writeStartup(path, StartupChoice{AtLogin: true, Hidden: false}); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if StartsHidden(path) {
		t.Error("opening at login was taken to mean opening hidden")
	}

	_ = writeStartup(path, StartupChoice{AtLogin: false, Hidden: true})
	if !StartsHidden(path) {
		t.Error("opening hidden was not remembered")
	}
}

/*
Neither happens to somebody who has not asked.

An application that added itself to a login, or opened invisibly, without being
asked has taken a decision about somebody's machine that is not its to take.
*/
func TestNeitherStartupChoiceIsOnByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nothing-here.json")

	choice, found := readStartup(path)
	if found {
		t.Error("a file that does not exist was read")
	}
	if choice.AtLogin || choice.Hidden {
		t.Errorf("the default is %+v", choice)
	}

	/*
		And through the function the application actually calls, not only the
		one behind it. Checking the reader alone left a default of "start
		hidden" undetected — an application that opened invisibly to somebody
		who never asked it to.
	*/
	if StartsHidden(path) {
		t.Error("TDC would open hidden for somebody who never asked it to")
	}
	if StartsHidden("") {
		t.Error("TDC would open hidden with nowhere to have recorded that")
	}
}
