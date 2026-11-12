package remotecontrol

import "strings"

/*
The keys a controller can press, by KeyboardEvent.code — the physical key, not
the character, so Shift and the sharer's own layout decide what it types, as
they would for somebody at the keyboard.

Each with what it is called on each platform: an X keysym, a Windows virtual
key, a Mac virtual keycode (0xffff where a Mac has no such key). A key not
listed is not injected.
*/
type key struct {
	keysym uint32
	vk     uint16
	mac    uint16
}

const noMacKey = 0xffff

var keys = func() map[string]key {
	table := map[string]key{
		"Enter": {0xff0d, 0x0d, 0x24}, "Tab": {0xff09, 0x09, 0x30}, "Backspace": {0xff08, 0x08, 0x33},
		"Escape": {0xff1b, 0x1b, 0x35}, "Space": {0x20, 0x20, 0x31}, "Delete": {0xffff, 0x2e, 0x75},
		"Insert": {0xff63, 0x2d, 0x72}, "Home": {0xff50, 0x24, 0x73}, "End": {0xff57, 0x23, 0x77},
		"PageUp": {0xff55, 0x21, 0x74}, "PageDown": {0xff56, 0x22, 0x79},
		"ArrowLeft": {0xff51, 0x25, 0x7b}, "ArrowUp": {0xff52, 0x26, 0x7e},
		"ArrowRight": {0xff53, 0x27, 0x7c}, "ArrowDown": {0xff54, 0x28, 0x7d},
		"ShiftLeft": {0xffe1, 0xa0, 0x38}, "ShiftRight": {0xffe2, 0xa1, 0x3c},
		"ControlLeft": {0xffe3, 0xa2, 0x3b}, "ControlRight": {0xffe4, 0xa3, 0x3e},
		"AltLeft": {0xffe9, 0xa4, 0x3a}, "AltRight": {0xffea, 0xa5, 0x3d},
		"MetaLeft": {0xffeb, 0x5b, 0x37}, "MetaRight": {0xffec, 0x5c, 0x36},
		"CapsLock": {0xffe5, 0x14, 0x39}, "Pause": {0xff13, 0x13, noMacKey},
		"Minus": {'-', 0xbd, 0x1b}, "Equal": {'=', 0xbb, 0x18},
		"BracketLeft": {'[', 0xdb, 0x21}, "BracketRight": {']', 0xdd, 0x1e},
		"Backslash": {'\\', 0xdc, 0x2a}, "Semicolon": {';', 0xba, 0x29}, "Quote": {'\'', 0xde, 0x27},
		"Backquote": {'`', 0xc0, 0x32}, "Comma": {',', 0xbc, 0x2b}, "Period": {'.', 0xbe, 0x2f},
		"Slash": {'/', 0xbf, 0x2c},
	}
	macLetters := map[rune]uint16{
		'A': 0x00, 'S': 0x01, 'D': 0x02, 'F': 0x03, 'H': 0x04, 'G': 0x05, 'Z': 0x06, 'X': 0x07,
		'C': 0x08, 'V': 0x09, 'B': 0x0b, 'Q': 0x0c, 'W': 0x0d, 'E': 0x0e, 'R': 0x0f, 'Y': 0x10,
		'T': 0x11, 'O': 0x1f, 'U': 0x20, 'I': 0x22, 'P': 0x23, 'L': 0x25, 'J': 0x26, 'K': 0x28,
		'N': 0x2d, 'M': 0x2e,
	}
	for letter, mac := range macLetters {
		table["Key"+string(letter)] = key{uint32(letter - 'A' + 'a'), uint16(letter), mac}
	}
	macDigits := []uint16{0x1d, 0x12, 0x13, 0x14, 0x15, 0x17, 0x16, 0x1a, 0x1c, 0x19}
	for digit := range 10 {
		table["Digit"+string(rune('0'+digit))] = key{uint32('0' + digit), uint16('0' + digit), macDigits[digit]}
	}
	macF := []uint16{0x7a, 0x78, 0x63, 0x76, 0x60, 0x61, 0x62, 0x64, 0x65, 0x6d, 0x67, 0x6f}
	for n := range 12 {
		table["F"+itoa(n+1)] = key{uint32(0xffbe + n), uint16(0x70 + n), macF[n]}
	}
	return table
}()

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "1" + string(rune('0'+n-10))
}

func knownKey(code string) bool {
	_, ok := keys[code]
	return ok
}

/*
chord is the key kept for the sharer, with the modifiers that must be held
with it: Pause on its own where keyboards have one, and on a Mac, which has
none, Control-Option-Command-Escape. The controller can never press it — it is
refused before it reaches the system — so every press of it this machine sees
is the sharer's own.
*/
type chord struct {
	code      string
	modifiers []string
}

// reserved is whether pressing code now, with the controller's modifiers
// held, would be the sharer's key.
func reserved(code string, held map[string]bool) bool {
	if code != revokeChord.code {
		return false
	}
	for _, modifier := range revokeChord.modifiers {
		if !held[modifier+"Left"] && !held[modifier+"Right"] {
			return false
		}
	}
	return true
}

// revokeKeyName is the chord as somebody would read it.
func revokeKeyName() string {
	return strings.Join(append(append([]string{}, revokeChord.modifiers...), revokeChord.code), "+")
}
