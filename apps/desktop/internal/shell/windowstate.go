package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

/*
Where the window was, and whether to open at login.

Somebody who puts an application on their second monitor and sizes it to half
the screen has made a decision, and making it again on every launch is the sort
of small friction that makes software feel careless.

The awkward part is that monitors come and go. A position remembered from a
docked laptop is off-screen when that laptop is on a train, and a window
restored to it is a window nobody can see or reach — so a remembered position
is checked against the displays that are actually connected before it is used.
*/

/*
StartupChoice is how the application should open.

Separate from the placement because it is a preference rather than a memory:
where the window was is something observed, and whether to show it at all is
something chosen.
*/
type StartupChoice struct {
	// AtLogin opens TDC when somebody logs in.
	AtLogin bool `json:"atLogin"`
	/*
		Hidden opens it to the tray rather than to a window.

		The point of opening at login for a chat application: it is there and
		receiving, without a window appearing over whatever somebody actually
		sat down to do.
	*/
	Hidden bool `json:"hidden"`
}

// Placement is where the window was when it was last closed.
type Placement struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
	// Maximised, which is a state rather than a size: a maximised window
	// restored as a large one is not the same thing.
	Maximised bool `json:"maximised"`
}

/*
Valid reports whether a placement holds enough to act on.

A zero size is what an unwritten or half-written file gives, and restoring to it
would produce a window with no area.
*/
func (p Placement) Valid() bool {
	return p.Width > 0 && p.Height > 0
}

/*
OnScreen reports whether enough of this placement falls on a connected display
for somebody to reach the window.

Not whether it fits: a window slightly off the edge is ordinary and somebody
can drag it back. What matters is whether enough of it is visible to be
grabbed — a title bar entirely on a monitor that is no longer there leaves a
window that can only be recovered by deleting a file nobody knows about.
*/
func (p Placement) OnScreen(screens []application.Rect) bool {
	/*
		A strip along the top, which is what a person actually grabs. Judging
		by total overlap would accept a window whose only visible part is a
		bottom corner.
	*/
	const grabbable = 48

	for _, screen := range screens {
		overlapWidth := min(p.X+p.Width, screen.X+screen.Width) - max(p.X, screen.X)
		overlapHeight := min(p.Y+grabbable, screen.Y+screen.Height) - max(p.Y, screen.Y)

		// A third of the window's width, so a sliver at the edge is not taken
		// as good enough.
		if overlapWidth >= p.Width/3 && overlapHeight > 0 {
			return true
		}
	}

	return false
}

/*
WindowState remembers where the window was.

Saved on a timer rather than on every move: dragging a window emits a great
many events, and writing a file for each would be writing hundreds of times to
reach one final position.
*/
type WindowState struct {
	window application.Window
	path   string

	mu      sync.Mutex
	pending *time.Timer
}

/*
RestoreWindowState puts the window back where it was, if that is still
somewhere it can be seen.

Called after the window is created. Returns what was restored, or the zero
placement where nothing was.
*/
func RestoreWindowState(window application.Window, path string, screens []application.Rect) Placement {
	state := &WindowState{window: window, path: path}

	saved, ok := readPlacement(path)
	if ok && saved.Valid() && saved.OnScreen(screens) {
		if saved.Maximised {
			/*
				Maximised is restored as maximised rather than as the size it
				happened to have: somebody who maximised a window wants it to
				fill whatever screen it opens on, which may not be the one it
				was on.
			*/
			window.SetBounds(application.Rect{
				X: saved.X, Y: saved.Y, Width: saved.Width, Height: saved.Height,
			})
			window.Maximise()
		} else {
			window.SetBounds(application.Rect{
				X: saved.X, Y: saved.Y, Width: saved.Width, Height: saved.Height,
			})
		}
	} else {
		// Nothing usable. The window keeps the size it was created with, which
		// is a sensible default rather than a remembered mistake.
		saved = Placement{}
	}

	state.watch()

	return saved
}

/*
watch records the window's placement as it changes.

Debounced: a drag emits a move event per frame, and this is a file.
*/
func (s *WindowState) watch() {
	for _, event := range []events.WindowEventType{
		events.Common.WindowDidMove,
		events.Common.WindowDidResize,
	} {
		s.window.OnWindowEvent(event, func(*application.WindowEvent) { s.saveSoon() })
	}
}

// saveSoon writes the placement once the window has stopped moving.
func (s *WindowState) saveSoon() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending != nil {
		s.pending.Stop()
	}
	s.pending = time.AfterFunc(time.Second, s.save)
}

func (s *WindowState) save() {
	x, y := s.window.Position()
	width, height := s.window.Size()

	writePlacement(s.path, Placement{
		X: x, Y: y, Width: width, Height: height,
		Maximised: s.window.IsMaximised(),
	})
}

// readPlacement returns what was saved, and whether anything was.
func readPlacement(path string) (Placement, bool) {
	if path == "" {
		return Placement{}, false
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return Placement{}, false
	}

	var saved Placement
	if err := json.Unmarshal(contents, &saved); err != nil {
		return Placement{}, false
	}

	return saved, true
}

/*
writePlacement saves it.

Failures are ignored: a window position that could not be written is a window
that opens where it was created next time, which nobody would report — and an
error about it would interrupt whatever somebody was doing.
*/
func writePlacement(path string, placement Placement) {
	if path == "" || !placement.Valid() {
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	encoded, err := json.Marshal(placement)
	if err != nil {
		return
	}

	_ = os.WriteFile(path, encoded, 0o644)
}

/*
ScreenBounds is the connected displays, as this needs them.

Taken as a plain list rather than reading the application, so the check that
uses it can be tested without a desktop session.
*/
func ScreenBounds(app *application.App) []application.Rect {
	var bounds []application.Rect

	for _, screen := range app.Screen.GetAll() {
		bounds = append(bounds, screen.WorkArea)
	}

	return bounds
}

/*
StartupService is opening at login, as the interface sees it.

Opt-in, and off by default: an application that added itself to somebody's
login without being asked has taken a decision about their machine that is not
its to take.
*/
type StartupService struct {
	app  *application.App
	path string
}

// NewStartupService builds it.
func NewStartupService(app *application.App, path string) *StartupService {
	return &StartupService{app: app, path: path}
}

/*
StartsHidden reports whether TDC should open to the tray.

Read from a file rather than asked of the platform: opening hidden is this
application's own behaviour, where opening at login is a registration the
operating system holds.
*/
func StartsHidden(path string) bool {
	choice, _ := readStartup(path)
	return choice.Hidden
}

// StartupSettings reports both choices, for the interface to show.
func (s *StartupService) StartupSettings() StartupChoice {
	choice, _ := readStartup(s.path)
	// Asked of the platform rather than trusted from the file, since somebody
	// may have removed the registration themselves.
	choice.AtLogin = s.OpensAtLogin()
	return choice
}

/*
SetStartsHidden records whether to open to the tray.

Refused where there is no tray to open to: hiding a window with no icon to
bring it back leaves an application somebody can neither see nor stop.
*/
func (s *StartupService) SetStartsHidden(hidden bool) error {
	choice, _ := readStartup(s.path)
	choice.Hidden = hidden
	return writeStartup(s.path, choice)
}

// readStartup returns the saved choice, and whether there was one.
func readStartup(path string) (StartupChoice, bool) {
	if path == "" {
		return StartupChoice{}, false
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return StartupChoice{}, false
	}

	var saved StartupChoice
	if err := json.Unmarshal(contents, &saved); err != nil {
		return StartupChoice{}, false
	}

	return saved, true
}

// writeStartup saves it.
func writeStartup(path string, choice StartupChoice) error {
	if path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.Marshal(choice)
	if err != nil {
		return err
	}

	return os.WriteFile(path, encoded, 0o644)
}

/*
OpensAtLogin reports whether it currently does.

Asked of the platform rather than remembered, because somebody may have
removed the registration themselves — through their desktop's own settings, or
by deleting the file — and an application insisting otherwise would be wrong
about their machine.
*/
func (s *StartupService) OpensAtLogin() bool {
	enabled, err := s.app.Autostart.IsEnabled()
	if err != nil {
		return false
	}
	return enabled
}

/*
SetOpensAtLogin turns it on or off.

Returns an error where the platform has no way to do it — a desktop with no
autostart directory, say — so the interface can say that rather than showing a
switch that silently does nothing.
*/
func (s *StartupService) SetOpensAtLogin(wanted bool) error {
	if wanted {
		if err := s.app.Autostart.EnableWithOptions(application.AutostartOptions{
			// The application id, so the registration is recognisably this
			// application in somebody's login settings rather than a slug.
			Identifier: "net.anorebel.tdc",
		}); err != nil {
			return err
		}
	} else if err := s.app.Autostart.Disable(); err != nil {
		return err
	}

	// Remembered too, so the interface can show both choices without asking
	// the platform twice.
	choice, _ := readStartup(s.path)
	choice.AtLogin = wanted
	return writeStartup(s.path, choice)
}
