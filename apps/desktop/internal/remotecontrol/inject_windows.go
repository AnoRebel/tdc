//go:build windows

package remotecontrol

import (
	"runtime"
	"syscall"
	"unsafe"
)

/*
On Windows, input goes in through SendInput, called directly in user32 — no
library needed. Windows does not let a program running without elevation send
input to one running as administrator, so a controller cannot drive an
elevated window; everything else they can.
*/

var revokeChord = chord{code: "Pause"}

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	sendInput        = user32.NewProc("SendInput")
	getSystemMetrics = user32.NewProc("GetSystemMetrics")
	registerHotKey   = user32.NewProc("RegisterHotKey")
	unregisterHotKey = user32.NewProc("UnregisterHotKey")
	getMessage       = user32.NewProc("GetMessageW")
	postThreadMsg    = user32.NewProc("PostThreadMessageW")
	currentThreadID  = kernel32.NewProc("GetCurrentThreadId")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseMove       = 0x0001
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	mouseHWheel     = 0x1000
	mouseVirtual    = 0x4000
	mouseAbsolute   = 0x8000

	keyExtended = 0x0001
	keyUp       = 0x0002

	wmHotkey    = 0x0312
	wmQuit      = 0x0012
	modNoRepeat = 0x4000
)

type mouseInput struct {
	kind uint32
	dx   int32
	dy   int32
	data uint32
	flag uint32
	time uint32
	info uintptr
}

type keyboardInput struct {
	kind uint32
	vk   uint16
	scan uint16
	flag uint32
	time uint32
	info uintptr
	_    [8]byte // INPUT is as large as its largest member, the mouse's.
}

// Platform says what this machine can do, and how to start doing it.
func Platform() (func() Capability, func() (Injector, error)) {
	return func() Capability { return Capability{Available: true, RevokeKey: revokeKeyName()} },
		func() (Injector, error) { return windowsInput{}, nil }
}

type windowsInput struct{}

func (windowsInput) Physical() bool { return true }

func send(input unsafe.Pointer, size uintptr) error {
	sent, _, err := sendInput.Call(1, uintptr(input), size)
	if sent == 0 {
		return err
	}
	return nil
}

func sendMouse(input mouseInput) error {
	input.kind = inputMouse
	return send(unsafe.Pointer(&input), unsafe.Sizeof(input))
}

func metric(index uintptr) int {
	value, _, _ := getSystemMetrics.Call(index)
	return int(int32(value))
}

func (windowsInput) Move(x, y int) error {
	// Absolute positions are given across the whole virtual desktop, 0–65535.
	left, top := metric(76), metric(77)
	width, height := max(metric(78)-1, 1), max(metric(79)-1, 1)
	return sendMouse(mouseInput{
		dx:   int32((x - left) * 65535 / width),
		dy:   int32((y - top) * 65535 / height),
		flag: mouseMove | mouseAbsolute | mouseVirtual,
	})
}

func (windowsInput) Button(button int, down bool) error {
	flags := [3][2]uint32{
		{mouseLeftUp, mouseLeftDown}, {mouseMiddleUp, mouseMiddleDown}, {mouseRightUp, mouseRightDown},
	}
	pressed := 0
	if down {
		pressed = 1
	}
	return sendMouse(mouseInput{flag: flags[button][pressed]})
}

func (windowsInput) Wheel(dx, dy int) error {
	if dy != 0 {
		// The browser's down is positive; the wheel's is negative.
		if err := sendMouse(mouseInput{flag: mouseWheel, data: uint32(int32(-dy * 120))}); err != nil {
			return err
		}
	}
	if dx != 0 {
		return sendMouse(mouseInput{flag: mouseHWheel, data: uint32(int32(dx * 120))})
	}
	return nil
}

// Keys Windows needs marked as extended to tell them from the keypad's.
var extended = map[string]bool{
	"Insert": true, "Delete": true, "Home": true, "End": true, "PageUp": true, "PageDown": true,
	"ArrowLeft": true, "ArrowUp": true, "ArrowRight": true, "ArrowDown": true,
	"ControlRight": true, "AltRight": true, "MetaLeft": true, "MetaRight": true,
}

func (windowsInput) Key(code string, down bool) error {
	input := keyboardInput{kind: inputKeyboard, vk: keys[code].vk}
	if extended[code] {
		input.flag |= keyExtended
	}
	if !down {
		input.flag |= keyUp
	}
	return send(unsafe.Pointer(&input), unsafe.Sizeof(input))
}

/*
WatchRevokeKey registers the reserved key as a system-wide hotkey, on a thread
of its own that waits for it, so it reaches this whichever window has focus.
*/
func (windowsInput) WatchRevokeKey(revoke func()) func() {
	threads := make(chan uintptr, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		thread, _, _ := currentThreadID.Call()
		ok, _, _ := registerHotKey.Call(0, 1, modNoRepeat, uintptr(keys[revokeChord.code].vk))
		threads <- thread
		if ok == 0 {
			return
		}
		defer unregisterHotKey.Call(0, 1) //nolint:errcheck // Nothing to do if it fails.
		var message struct {
			hwnd    uintptr
			message uint32
			wParam  uintptr
			lParam  uintptr
			time    uint32
			x, y    int32
			private uint32
		}
		for {
			got, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(got) <= 0 {
				return
			}
			if message.message == wmHotkey {
				revoke()
			}
		}
	}()
	thread := <-threads
	return func() { postThreadMsg.Call(thread, wmQuit, 0, 0) } //nolint:errcheck // A thread already gone needs no message.
}
