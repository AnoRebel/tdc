package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

/*
How solid the window is.

Some people want an application that shows a little of what is behind it, and
some find that unreadable. Neither is wrong, so it is a setting — and it is off
by default, because a chat window somebody cannot read is worse than a plain
one they can.

The window is always created able to be translucent; what the setting changes
is the alpha of its background. Creating it solid would make this need a
restart, and a preference that needs a restart is one people set once and never
revisit.
*/

// EventSetTranslucency is the interface asking for a different opacity.
const EventSetTranslucency = "window:translucency"

/*
Opacity is how solid the window should be, from 0 to 100.

A percentage rather than a fraction because it is a thing somebody chooses on a
slider, and 85 reads better than 0.85 in a settings file.
*/
type Opacity int

const (
	// Solid is the default: no translucency at all.
	Solid Opacity = 100
	// leastReadable is as transparent as this will go. Below roughly half,
	// text on any background becomes a struggle whatever the theme.
	leastReadable Opacity = 50
)

// Clamp brings an opacity into the range this will actually apply.
func (o Opacity) Clamp() Opacity {
	switch {
	case o > Solid:
		return Solid
	case o < leastReadable:
		return leastReadable
	default:
		return o
	}
}

/*
Translucency applies and remembers how solid the window is.

The value is kept in a file rather than asked of the interface at startup: the
window is drawn before any JavaScript runs, and a window that flashed solid
before turning translucent would be worse than one that never moved.
*/
type Translucency struct {
	window application.Window
	path   string

	mu      sync.RWMutex
	opacity Opacity
	// The theme's own dark ground, which the alpha is applied to.
	red, green, blue uint8
}

// NewTranslucency restores whatever was chosen and applies it.
func NewTranslucency(window application.Window, path string, red, green, blue uint8) *Translucency {
	translucency := &Translucency{
		window: window, path: path, opacity: Solid,
		red: red, green: green, blue: blue,
	}

	if saved, ok := readOpacity(path); ok {
		translucency.opacity = saved
	}
	translucency.apply()

	return translucency
}

// Opacity reports how solid the window currently is.
func (t *Translucency) Opacity() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return int(t.opacity)
}

/*
SetOpacity changes it and remembers the choice.

Clamped rather than refused: a value outside the range is somebody dragging a
slider to its end, not an error worth reporting.
*/
func (t *Translucency) SetOpacity(percent int) {
	t.mu.Lock()
	t.opacity = Opacity(percent).Clamp()
	path, opacity := t.path, t.opacity
	t.mu.Unlock()

	t.apply()
	writeOpacity(path, opacity)
}

// apply paints the window at the current opacity.
func (t *Translucency) apply() {
	t.mu.RLock()
	opacity, red, green, blue := t.opacity, t.red, t.green, t.blue
	t.mu.RUnlock()

	t.window.SetBackgroundColour(application.RGBA{
		Red: red, Green: green, Blue: blue,
		// 255 is fully opaque, so a percentage maps onto it directly.
		Alpha: uint8(int(opacity) * 255 / 100),
	})
}

// readOpacity returns what was saved, and whether anything was.
func readOpacity(path string) (Opacity, bool) {
	if path == "" {
		return Solid, false
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return Solid, false
	}

	var saved struct {
		Opacity int `json:"opacity"`
	}
	if err := json.Unmarshal(contents, &saved); err != nil {
		return Solid, false
	}

	return Opacity(saved.Opacity).Clamp(), true
}

/*
writeOpacity saves the choice.

Failures are ignored: a preference that could not be written is a preference
somebody sets again, where an error about it interrupts what they were doing to
report something they cannot act on.
*/
func writeOpacity(path string, opacity Opacity) {
	if path == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	encoded, err := json.Marshal(struct {
		Opacity int `json:"opacity"`
	}{Opacity: int(opacity)})
	if err != nil {
		return
	}

	_ = os.WriteFile(path, encoded, 0o644)
}

// TranslucencyService is what the interface calls.
type TranslucencyService struct{ translucency *Translucency }

// NewTranslucencyService wraps it for binding.
func NewTranslucencyService(t *Translucency) *TranslucencyService {
	return &TranslucencyService{translucency: t}
}

// WindowOpacity reports how solid the window is, as a percentage.
func (s *TranslucencyService) WindowOpacity() int { return s.translucency.Opacity() }

// SetWindowOpacity changes it.
func (s *TranslucencyService) SetWindowOpacity(percent int) { s.translucency.SetOpacity(percent) }
