package realtime_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/realtime"
)

/*
Carrying an event between processes.

The failure this prevents is quiet: somebody connected to the second node hears
nothing about a message sent through the first, and the only symptom is a
conversation that appears to have stopped until they reload. Nothing errors, so
nothing in a log says so.

Two brokers over one database are two nodes. They have separate connections and
share nothing but Postgres, which is what two processes behind a load balancer
have.
*/

func brokerPool(t *testing.T) *db.Pool {
	t.Helper()

	url := dbtest.URL(t, "broker")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, quiet); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, quiet)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// heard collects announcements a node received.
type heard struct {
	mu   sync.Mutex
	seen []realtime.Announcement
}

func (h *heard) record(announcement realtime.Announcement) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, announcement)
}

func (h *heard) all() []realtime.Announcement {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]realtime.Announcement(nil), h.seen...)
}

// waitFor gives a listener a moment to receive something, without sleeping for
// a fixed time that is either flaky or slow.
func (h *heard) waitFor(t *testing.T, count int) []realtime.Announcement {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := h.all(); len(got) >= count {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h.all()
}

/*
TestAnEventReachesAnotherNode is the whole point.

One node announces, the other hears. Without it, half a community sees a
conversation and the other half sees silence.
*/
func TestAnEventReachesAnotherNode(t *testing.T) {
	pool := brokerPool(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodeOne := realtime.NewPostgresBroker(pool.Pool, "node-one", quiet)
	nodeTwo := realtime.NewPostgresBroker(pool.Pool, "node-two", quiet)

	listening := &heard{}
	go func() { _ = nodeTwo.Listen(ctx, listening.record) }()

	// The listener has to be established before the announcement, as it would
	// be on a running server that started listening before serving.
	time.Sleep(200 * time.Millisecond)

	announcement := realtime.Announcement{
		EventID: "evt-" + uuid.NewString(),
		RoomID:  uuid.NewString(),
	}
	if err := nodeOne.Announce(ctx, announcement); err != nil {
		t.Fatalf("announcing: %v", err)
	}

	received := listening.waitFor(t, 1)
	if len(received) == 0 {
		t.Fatal("the other node heard nothing — a message sent on one node reaches nobody on another")
	}
	if received[0].EventID != announcement.EventID {
		t.Errorf("heard about %q, expected %q", received[0].EventID, announcement.EventID)
	}
	if received[0].Origin != "node-one" {
		t.Errorf("the announcement says it came from %q", received[0].Origin)
	}
}

/*
TestANodeIgnoresItsOwnAnnouncement.

NOTIFY reaches every listener including the sender, and the sender has already
delivered to its own connections. Acting on it again would show everybody on
that node the same message twice.
*/
func TestANodeIgnoresItsOwnAnnouncement(t *testing.T) {
	pool := brokerPool(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := realtime.NewPostgresBroker(pool.Pool, "the-only-node", quiet)

	listening := &heard{}
	go func() { _ = node.Listen(ctx, listening.record) }()
	time.Sleep(200 * time.Millisecond)

	if err := node.Announce(ctx, realtime.Announcement{
		EventID: "evt-" + uuid.NewString(),
		RoomID:  uuid.NewString(),
	}); err != nil {
		t.Fatalf("announcing: %v", err)
	}

	// Long enough that it would have arrived had it been going to.
	time.Sleep(500 * time.Millisecond)

	if got := listening.all(); len(got) != 0 {
		t.Errorf("a node acted on its own announcement, so everybody on it would see the message twice: %v", got)
	}
}

/*
TestASingleNodeNeedsNoBroker.

The ordinary deployment. Alone delivers nothing and blocks until shutdown, which
is right: the connections that should see a message are in the same memory as
the code that stored it, and a round trip through the database to tell them
would be slower and no more correct.
*/
func TestASingleNodeNeedsNoBroker(t *testing.T) {
	alone := realtime.Alone{}

	// Announcing succeeds and does nothing.
	if err := alone.Announce(context.Background(), realtime.Announcement{EventID: "anything"}); err != nil {
		t.Errorf("announcing on a single node: %v", err)
	}

	// Listening blocks until the context ends rather than returning at once,
	// which would make the caller's goroutine spin.
	ctx, cancel := context.WithCancel(context.Background())

	returned := make(chan struct{})
	go func() {
		_ = alone.Listen(ctx, func(realtime.Announcement) {
			t.Error("a single-node broker delivered an announcement")
		})
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("Listen returned before the context ended, so the caller would spin")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Error("Listen did not return when the context ended, so shutdown would hang")
	}

	if err := alone.Close(); err != nil {
		t.Errorf("closing: %v", err)
	}
}

/*
TestAnOversizedAnnouncementIsRefusedHere.

NOTIFY carries at most eight thousand bytes, and a payload past it is a database
error during a message send. The announcement carries three identifiers so it
cannot approach that — but if a field is added later, this fails where somebody
can read why.
*/
func TestAnOversizedAnnouncementIsRefusedHere(t *testing.T) {
	pool := brokerPool(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	node := realtime.NewPostgresBroker(pool.Pool, "node", quiet)

	oversized := realtime.Announcement{
		EventID: string(make([]byte, 9000)),
		RoomID:  uuid.NewString(),
	}

	if err := node.Announce(context.Background(), oversized); err == nil {
		t.Error("an announcement too large for NOTIFY was sent anyway")
	}
}
