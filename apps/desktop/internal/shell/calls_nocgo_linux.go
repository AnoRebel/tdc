//go:build linux && !cgo

package shell

import "unsafe"

// Without cgo there is no web view to switch calls on in; tests build this way.
func enableWebRTC(unsafe.Pointer) int { return 0 }
