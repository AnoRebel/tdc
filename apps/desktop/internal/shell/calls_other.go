//go:build !linux

package shell

import "unsafe"

// Elsewhere the web view places calls as it comes, and nothing is known missing.
func missingForCalls() string { return "" }

func enableWebRTC(unsafe.Pointer) int { return 0 }
