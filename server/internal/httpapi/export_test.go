package httpapi

import "time"

// SetRingTimeout shortens how long a ring lasts, so a test of a missed call
// does not wait out the real one. It returns the previous value to restore.
func SetRingTimeout(timeout time.Duration) time.Duration {
	previous := ringTimeout
	ringTimeout = timeout
	return previous
}
