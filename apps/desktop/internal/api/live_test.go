package api

import (
	"testing"
)

// Sending before anything is open is refused rather than panicking on a nil
// connection — the webview's machinery sends on its own schedule.
func TestSendingWithoutAConnectionIsRefused(t *testing.T) {
	bridge := NewLiveBridge(New(), func(string, ...any) bool { return true }, nil)

	if err := bridge.Send([]byte(`{"action":"ping"}`)); err == nil {
		t.Fatal("sending with no connection was accepted")
	}
}

// Closing twice, or closing one that never opened, is safe: the webview calls
// it on teardown without tracking whether a connection was ever made.
func TestClosingIsSafeWhenNothingIsOpen(t *testing.T) {
	bridge := NewLiveBridge(New(), func(string, ...any) bool { return true }, nil)
	bridge.Close()
	bridge.Close()
}
