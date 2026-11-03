package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
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

/*
Reconnecting does not tell the webview the new connection closed.

The webview reconnects by calling Connect again, which ends the old socket.
That socket's reader then fails — after the new one has opened — and saying
"closed" for it had the webview reconnect again, in a loop that never kept a
connection long enough to deliver anything.
*/
func TestReconnectingReportsNoCloseForTheReplacedConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Held open until the client goes.
		_, _, _ = conn.Read(context.Background())
	}))
	t.Cleanup(server.Close)

	service := New()
	if _, err := service.UseServer(server.URL); err != nil {
		t.Fatalf("using the server: %v", err)
	}
	var mu sync.Mutex
	var events []string
	bridge := NewLiveBridge(service, func(name string, _ ...any) bool {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, name)
		return true
	}, nil)
	t.Cleanup(bridge.Close)

	for range 2 {
		if err := bridge.Connect(); err != nil {
			t.Fatalf("connecting: %v", err)
		}
	}
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	for _, name := range events {
		if name == LiveCloseEvent {
			t.Fatalf("the webview was told a connection closed while one is open: %v", events)
		}
	}
}
