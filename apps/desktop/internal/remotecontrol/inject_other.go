//go:build !linux && !windows && !darwin

package remotecontrol

import "errors"

// Elsewhere there is no known way in, and remote control says so.

var revokeChord = chord{code: "Pause"}

// Platform says what this machine can do: here, nothing.
func Platform() (func() Capability, func() (Injector, error)) {
	return func() Capability { return Capability{Reason: ReasonPlatform, RevokeKey: revokeKeyName()} },
		func() (Injector, error) { return nil, errors.New("no input injection on this platform") }
}
