//go:build linux

package remotecontrol

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

/*
On Linux, input goes in through X11's XTEST extension, spoken in Go (xgb, BSD
licence) rather than through libXtst, so nothing more is needed on the machine
than an X server.

Wayland is the honest limit. A Wayland compositor does not let one program move
the pointer or type into another; XTEST under XWayland reaches only the X11
programs running there, not the desktop. The way in a Wayland desktop offers is
its remote-desktop portal, which asks the person itself and which this
application does not use yet, so in a Wayland session remote control is
refused and the window says why.
*/

var revokeChord = chord{code: "Pause"}

// Platform says what this machine can do, and how to start doing it.
func Platform() (func() Capability, func() (Injector, error)) {
	return probe, func() (Injector, error) { return openX11() }
}

func probe() Capability {
	capability := Capability{RevokeKey: revokeKeyName()}
	switch {
	case os.Getenv("XDG_SESSION_TYPE") == "wayland" || os.Getenv("WAYLAND_DISPLAY") != "":
		capability.Reason = ReasonWayland
	case os.Getenv("DISPLAY") == "":
		capability.Reason = ReasonNoDisplay
	default:
		if injector, err := openX11(); err != nil {
			capability.Reason = ReasonNoXTest
		} else {
			injector.conn.Close()
			capability.Available = true
		}
	}
	return capability
}

type x11 struct {
	mu   sync.Mutex
	conn *xgb.Conn
	root xproto.Window
	// keycodes by keysym, read once from the keyboard's own mapping.
	keycodes map[uint32]xproto.Keycode
}

func openX11() (*x11, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connecting to the X server: %w", err)
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("the X server has no XTEST: %w", err)
	}
	setup := xproto.Setup(conn)
	injector := &x11{conn: conn, root: setup.DefaultScreen(conn).Root, keycodes: map[uint32]xproto.Keycode{}}
	first, last := setup.MinKeycode, setup.MaxKeycode
	mapping, err := xproto.GetKeyboardMapping(conn, first, byte(last-first+1)).Reply()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading the keyboard mapping: %w", err)
	}
	per := int(mapping.KeysymsPerKeycode)
	for index, keysym := range mapping.Keysyms {
		// The first keysym of each key, unshifted, is what names it.
		if per > 0 && index%per == 0 && keysym != 0 {
			code := xproto.Keycode(int(first) + index/per)
			if _, seen := injector.keycodes[uint32(keysym)]; !seen {
				injector.keycodes[uint32(keysym)] = code
			}
		}
	}
	return injector, nil
}

func (x *x11) Physical() bool { return true }

func (x *x11) fake(kind, detail byte, rootX, rootY int16) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	return xtest.FakeInputChecked(x.conn, kind, detail, 0, x.root, rootX, rootY, 0).Check()
}

func (x *x11) Move(px, py int) error {
	return x.fake(xproto.MotionNotify, 0, int16(px), int16(py))
}

func (x *x11) Button(button int, down bool) error {
	kind := byte(xproto.ButtonRelease)
	if down {
		kind = xproto.ButtonPress
	}
	// X numbers them left, middle, right from one.
	return x.fake(kind, byte(button+1), 0, 0)
}

func (x *x11) Wheel(dx, dy int) error {
	click := func(button byte, times int) error {
		for range times {
			if err := x.fake(xproto.ButtonPress, button, 0, 0); err != nil {
				return err
			}
			if err := x.fake(xproto.ButtonRelease, button, 0, 0); err != nil {
				return err
			}
		}
		return nil
	}
	var err error
	switch {
	case dy < 0:
		err = click(4, -dy)
	case dy > 0:
		err = click(5, dy)
	}
	switch {
	case dx < 0:
		err = errors.Join(err, click(6, -dx))
	case dx > 0:
		err = errors.Join(err, click(7, dx))
	}
	return err
}

func (x *x11) Key(code string, down bool) error {
	keycode, ok := x.keycodes[keys[code].keysym]
	if !ok {
		return nil // Not on this keyboard: nothing to press.
	}
	kind := byte(xproto.KeyRelease)
	if down {
		kind = xproto.KeyPress
	}
	return x.fake(kind, byte(keycode), 0, 0)
}

/*
WatchRevokeKey grabs the reserved key on the whole display, on a connection of
its own, so pressing it reaches this and nothing else while control is given.
The controller cannot press it: Apply refuses it before it gets here.
*/
func (x *x11) WatchRevokeKey(revoke func()) func() {
	keycode, ok := x.keycodes[keys[revokeChord.code].keysym]
	if !ok {
		return func() {}
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return func() {}
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	if err := xproto.GrabKeyChecked(conn, true, root, xproto.ModMaskAny, keycode,
		xproto.GrabModeAsync, xproto.GrabModeAsync).Check(); err != nil {
		conn.Close()
		return func() {}
	}
	go func() {
		for {
			event, err := conn.WaitForEvent()
			if event == nil && err == nil {
				return // The connection closed.
			}
			if press, ok := event.(xproto.KeyPressEvent); ok && press.Detail == keycode {
				revoke()
			}
		}
	}()
	return func() { conn.Close() }
}
