/*
Package remotecontrol lets one person in a call drive the screen somebody is
sharing from this application, while that person allows it.

The browser cannot move a pointer or press a key on the machine it runs on;
this application can, through the operating system. That is the whole reason
it lives here, and also the reason for caution: input reaches whatever has
focus, not only TDC. So the rules are kept here, beside the one thing that
injects, rather than trusted to the window:

  - nothing is applied unless a share of a whole screen is in progress and
    one named person holds a grant the sharer gave, now;
  - granting somebody else, revoking, or the share ending ends the grant at
    once, and input after that is refused;
  - one key is never injected, whatever the controller sends, and pressing it
    on this machine's own keyboard revokes — so the sharer always has a way
    back that the controller cannot take away or press for them.

Where the platform cannot inject at all — a Wayland session, a Mac that has not
given this application Accessibility access, an unknown system — Capability
says so and why, and Grant refuses.
*/
package remotecontrol

import (
	"errors"
	"math"
	"sync"
)

// Reasons remote control is unavailable, which the window turns into words.
const (
	ReasonWayland       = "wayland"
	ReasonNoDisplay     = "no-display"
	ReasonNoXTest       = "no-xtest"
	ReasonAccessibility = "accessibility"
	ReasonPlatform      = "platform"
)

// Capability says whether this machine can be controlled, and why not.
type Capability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// RevokeKey names the key that takes control back, for the window to show.
	RevokeKey string `json:"revokeKey"`
}

// Event is one piece of a controller's input, as the window passes it on.
// Positions are fractions of the shared screen, from its top left.
type Event struct {
	Kind   string  `json:"kind"` // move, button, wheel, key
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button int     `json:"button"` // 0 left, 1 middle, 2 right
	Down   bool    `json:"down"`
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	// Code is the physical key, as KeyboardEvent.code names it.
	Code string `json:"code"`
}

// Rect is a screen's place on the desktop.
type Rect struct{ X, Y, Width, Height int }

// Screen is one display, in logical and in physical pixels.
type Screen struct {
	Logical, Physical Rect
	Primary           bool
}

// Injector puts input into the operating system.
type Injector interface {
	// Physical says whether positions are in physical pixels, rather than
	// the logical ones a scaled display shows.
	Physical() bool
	Move(x, y int) error
	Button(button int, down bool) error
	Wheel(dx, dy int) error
	Key(code string, down bool) error
	// WatchRevokeKey calls revoke whenever the sharer presses the reserved
	// key on this machine's keyboard, until stop is called.
	WatchRevokeKey(revoke func()) (stop func())
}

// State is what the window and the indicator are told whenever it changes.
type State struct {
	Sharing    bool   `json:"sharing"`
	Controller string `json:"controller,omitempty"`
	Name       string `json:"name,omitempty"`
	// Notice is what the sharer's screen says while control is given, in
	// their language, as the window wrote it.
	Notice string `json:"notice,omitempty"`
}

var (
	// ErrUnavailable is control asked for where it cannot be given.
	ErrUnavailable = errors.New("this computer cannot be controlled remotely")
	// ErrNotSharing is a grant with no whole screen being shared.
	ErrNotSharing = errors.New("no whole screen is being shared")
	// ErrNoGrant is input from somebody who does not hold control now.
	ErrNoGrant = errors.New("control has not been given to this person")
	// ErrReserved is the key kept for taking control back.
	ErrReserved = errors.New("that key is kept for the person sharing")
)

/*
Service is remote control as the window sees it. Only the sharer's window
calls Grant: the controller's input arrives on the sharer's side, from the
call's data channel, and is passed to Apply with who sent it.
*/
type Service struct {
	mu      sync.Mutex
	probe   func() Capability
	open    func() (Injector, error)
	screens func() []Screen
	changed func(State)

	injector Injector
	sharing  bool
	// The size of what is shared, and where it is on the desktop.
	shareWidth, shareHeight int
	target                  Rect
	controller              string
	name                    string
	// The controller's keys held down now, let go when control ends, and
	// read for the reserved chord.
	held map[string]bool
	// stopWatching ends the watch for the reserved key, while a grant holds.
	stopWatching func()
}

// New makes the service. probe is asked afresh each time — somebody may allow
// Accessibility access, or change session, while it runs — and open is called
// on the first grant.
func New(probe func() Capability, open func() (Injector, error), screens func() []Screen) *Service {
	return &Service{probe: probe, open: open, screens: screens, changed: func(State) {}, held: map[string]bool{}}
}

// OnChange is told every change of who controls the screen. Not for the window.
func OnChange(s *Service, changed func(State)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changed = changed
}

// Capability says whether this machine can be controlled.
func (s *Service) Capability() Capability {
	return s.probe()
}

/*
BeginShare says a whole screen is now being shared, of the given size in
pixels, so the right display can be found: the one that size, or the primary.
*/
func (s *Service) BeginShare(width, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sharing = true
	s.shareWidth, s.shareHeight = width, height
	s.target = s.pick(width, height)
	s.endGrant()
}

func (s *Service) pick(width, height int) Rect {
	physical := s.injector == nil || s.injector.Physical()
	choose := func(screen Screen) Rect {
		if physical {
			return screen.Physical
		}
		return screen.Logical
	}
	var screens []Screen
	if s.screens != nil {
		screens = s.screens()
	}
	for _, screen := range screens {
		if screen.Physical.Width == width && screen.Physical.Height == height {
			return choose(screen)
		}
	}
	for _, screen := range screens {
		if screen.Logical.Width == width && screen.Logical.Height == height {
			return choose(screen)
		}
	}
	for _, screen := range screens {
		if screen.Primary {
			return choose(screen)
		}
	}
	if len(screens) > 0 {
		return choose(screens[0])
	}
	return Rect{Width: width, Height: height}
}

// EndShare says the share stopped, which ends any control with it.
func (s *Service) EndShare() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sharing = false
	s.endGrant()
}

// Grant hands control to one person, taking it from anybody who had it.
// notice is what to show on the sharer's screen while they hold it.
func (s *Service) Grant(controller, name, notice string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.probe().Available {
		return ErrUnavailable
	}
	if !s.sharing {
		return ErrNotSharing
	}
	if controller == "" {
		return ErrNoGrant
	}
	if s.injector == nil {
		injector, err := s.open()
		if err != nil {
			return err
		}
		s.injector = injector
		// Positions were picked before it was known which kind it wants.
		s.target = s.pick(s.shareWidth, s.shareHeight)
	}
	s.endGrant()
	s.controller, s.name = controller, name
	s.stopWatching = s.injector.WatchRevokeKey(func() { go s.Revoke() })
	s.changed(State{Sharing: true, Controller: controller, Name: name, Notice: notice})
	return nil
}

// Revoke takes control back at once.
func (s *Service) Revoke() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endGrant()
}

// endGrant ends any grant, letting go of whatever the controller held down.
// Called with the lock held.
func (s *Service) endGrant() {
	if s.controller == "" {
		return
	}
	if s.injector != nil {
		for code := range s.held {
			_ = s.injector.Key(code, false)
		}
		for button := range 3 {
			_ = s.injector.Button(button, false)
		}
	}
	s.held = map[string]bool{}
	if s.stopWatching != nil {
		s.stopWatching()
		s.stopWatching = nil
	}
	s.controller, s.name = "", ""
	s.changed(State{Sharing: s.sharing})
}

// Apply puts one piece of input into the system, if its sender holds control.
func (s *Service) Apply(controller string, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sharing || s.controller == "" || controller != s.controller || s.injector == nil {
		return ErrNoGrant
	}
	switch event.Kind {
	case "move":
		return s.injector.Move(s.at(event.X, event.Y))
	case "button":
		if event.Button < 0 || event.Button > 2 {
			return nil
		}
		x, y := s.at(event.X, event.Y)
		if err := s.injector.Move(x, y); err != nil {
			return err
		}
		return s.injector.Button(event.Button, event.Down)
	case "wheel":
		return s.injector.Wheel(clampStep(event.DX), clampStep(event.DY))
	case "key":
		if reserved(event.Code, s.held) {
			return ErrReserved
		}
		if !knownKey(event.Code) {
			return nil
		}
		if err := s.injector.Key(event.Code, event.Down); err != nil {
			return err
		}
		if event.Down {
			s.held[event.Code] = true
		} else {
			delete(s.held, event.Code)
		}
	}
	return nil
}

// at turns a fraction of the shared screen into a place on the desktop,
// kept on the screen whatever was sent.
func (s *Service) at(x, y float64) (int, int) {
	clamp := func(v float64) float64 {
		if math.IsNaN(v) {
			return 0
		}
		return math.Min(1, math.Max(0, v))
	}
	return s.target.X + int(clamp(x)*float64(s.target.Width-1)),
		s.target.Y + int(clamp(y)*float64(s.target.Height-1))
}

// clampStep turns a wheel movement into a few notches at most.
func clampStep(delta float64) int {
	switch {
	case delta > 0:
		return int(math.Min(5, math.Ceil(delta/100)))
	case delta < 0:
		return int(math.Max(-5, math.Floor(delta/100)))
	}
	return 0
}
