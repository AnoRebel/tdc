//go:build darwin

package remotecontrol

/*
#cgo LDFLAGS: -framework ApplicationServices -framework Carbon
int tdcTrusted(void);
void tdcMouse(int type, int button, double x, double y);
void tdcWhere(double *x, double *y);
void tdcWheel(int dy, int dx);
void tdcKey(int code, int down, unsigned long long flags);
void tdcWatch(void);
void tdcUnwatch(void);
*/
import "C"

import (
	"sync"
)

/*
On macOS, input goes in as Quartz events. macOS lets a program do that only
once the person has allowed it under Privacy & Security → Accessibility; until
then remote control is refused, saying so. A Mac keyboard has no Pause key, so
the key kept for the sharer is Control-Option-Command-Escape.
*/

var revokeChord = chord{code: "Escape", modifiers: []string{"Control", "Alt", "Meta"}}

// Platform says what this machine can do, and how to start doing it.
func Platform() (func() Capability, func() (Injector, error)) {
	probe := func() Capability {
		capability := Capability{Available: C.tdcTrusted() == 1, RevokeKey: revokeKeyName()}
		if !capability.Available {
			capability.Reason = ReasonAccessibility
		}
		return capability
	}
	return probe, func() (Injector, error) { return &quartz{}, nil }
}

type quartz struct {
	mu      sync.Mutex
	buttons [3]bool
	flags   uint64
}

const (
	leftDown, leftUp, rightDown, rightUp, moved, leftDragged, rightDragged = 1, 2, 3, 4, 5, 6, 7
	otherDown, otherUp, otherDragged                                       = 25, 26, 27
)

// Quartz numbers buttons left, right, middle; the window, left, middle, right.
var quartzButton = [3]int{0, 2, 1}

func (q *quartz) Physical() bool { return false }

func (q *quartz) Move(x, y int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	kind, button := moved, 0
	switch {
	case q.buttons[0]:
		kind = leftDragged
	case q.buttons[2]:
		kind, button = rightDragged, 1
	case q.buttons[1]:
		kind, button = otherDragged, 2
	}
	C.tdcMouse(C.int(kind), C.int(button), C.double(x), C.double(y))
	return nil
}

func (q *quartz) Button(button int, down bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	kinds := [3][2]int{{leftUp, leftDown}, {otherUp, otherDown}, {rightUp, rightDown}}
	pressed := 0
	if down {
		pressed = 1
	}
	q.buttons[button] = down
	var x, y C.double
	C.tdcWhere(&x, &y)
	C.tdcMouse(C.int(kinds[button][pressed]), C.int(quartzButton[button]), x, y)
	return nil
}

func (q *quartz) Wheel(dx, dy int) error {
	// Quartz scrolls content up for a positive line count.
	C.tdcWheel(C.int(-dy), C.int(-dx))
	return nil
}

var modifierFlags = map[string]uint64{
	"ShiftLeft": 0x20000, "ShiftRight": 0x20000, "ControlLeft": 0x40000, "ControlRight": 0x40000,
	"AltLeft": 0x80000, "AltRight": 0x80000, "MetaLeft": 0x100000, "MetaRight": 0x100000,
}

func (q *quartz) Key(code string, down bool) error {
	mac := keys[code].mac
	if mac == noMacKey {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if flag, ok := modifierFlags[code]; ok {
		if down {
			q.flags |= flag
		} else {
			q.flags &^= flag
		}
	}
	pressed := 0
	if down {
		pressed = 1
	}
	C.tdcKey(C.int(mac), C.int(pressed), C.ulonglong(q.flags))
	return nil
}

var (
	watchMu sync.Mutex
	onKey   func()
)

//export tdcRevokeKeyPressed
func tdcRevokeKeyPressed() {
	watchMu.Lock()
	revoke := onKey
	watchMu.Unlock()
	if revoke != nil {
		revoke()
	}
}

func (q *quartz) WatchRevokeKey(revoke func()) func() {
	watchMu.Lock()
	onKey = revoke
	watchMu.Unlock()
	C.tdcWatch()
	return func() {
		C.tdcUnwatch()
		watchMu.Lock()
		onKey = nil
		watchMu.Unlock()
	}
}
