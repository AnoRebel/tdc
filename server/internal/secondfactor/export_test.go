package secondfactor

import "time"

/*
SetClock holds the service at a chosen moment.

TOTP is arithmetic on the current time, so a test that generated a code and then
checked it would sometimes straddle a window boundary and fail for reasons
nothing to do with what it was testing. Standing still removes that.

In an _test.go file, so it exists only when the tests are built and there is no
way to reach it from the running server.
*/
func SetClock(service *Service, now func() time.Time) {
	service.now = now
}
