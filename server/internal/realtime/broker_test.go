package realtime_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
	go func() { _ = nodeTwo.Listen(ctx, realtime.Receivers{Announcement: listening.record}) }()

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
	go func() { _ = node.Listen(ctx, realtime.Receivers{Announcement: listening.record}) }()
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
		_ = alone.Listen(ctx, realtime.Receivers{Announcement: func(realtime.Announcement) {
			t.Error("a single-node broker delivered an announcement")
		}})
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

// whispered collects the private frames a node was handed to deliver.
type whispered struct {
	mu     sync.Mutex
	frames []realtime.Frame
	users  []uuid.UUID
	// connections is how many this node reports delivering each one to.
	connections int
}

func (w *whispered) deliver(userID uuid.UUID, frame realtime.Frame) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frames = append(w.frames, frame)
	w.users = append(w.users, userID)
	return w.connections
}

func (w *whispered) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.frames)
}

func listenPrivately(ctx context.Context, broker *realtime.PostgresBroker, into *whispered) {
	go func() {
		_ = broker.Listen(ctx, realtime.Receivers{
			Announcement: func(realtime.Announcement) {},
			Private:      into.deliver,
		})
	}()
}

/*
A private answer reaches the person on another node, whole, however long.

Longer than one notification can carry on purpose: an answer of the longest
length a program may send, wrapped in its frame, is past NOTIFY's limit, and a
delivery that worked until somebody wrote a long enough answer would be worse
than none. And the sender learns how many connections received it, which is
what lets the program be told the truth.
*/
func TestAPrivateFrameReachesAnotherNodeWhole(t *testing.T) {
	pool := brokerPool(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodeOne := realtime.NewPostgresBroker(pool.Pool, "node-one", quiet)
	nodeTwo := realtime.NewPostgresBroker(pool.Pool, "node-two", quiet)
	nodeThree := realtime.NewPostgresBroker(pool.Pool, "node-three", quiet)

	// The sender listens too, as every node does: it must not deliver its own
	// frame a second time, and must hear the others acknowledge.
	onOne, onTwo, onThree := &whispered{connections: 5}, &whispered{connections: 2}, &whispered{connections: 1}
	listenPrivately(ctx, nodeOne, onOne)
	listenPrivately(ctx, nodeTwo, onTwo)
	listenPrivately(ctx, nodeThree, onThree)
	time.Sleep(200 * time.Millisecond)

	body := bytes.Repeat([]byte("ñ"), 4000) // 8000 bytes, the longest answer
	payload, _ := json.Marshal(map[string]string{"body": string(body)})
	frame := realtime.Frame{Type: realtime.FramePrivate, RoomID: uuid.NewString(), Payload: payload}
	person := uuid.New()

	started := time.Now()
	delivered, err := nodeOne.Whisper(ctx, person, frame, 3, 5*time.Second)
	if err != nil {
		t.Fatalf("whispering: %v", err)
	}
	if delivered != 3 {
		t.Errorf("the sender was told %d connections received it, want 3", delivered)
	}
	// Answered when the acknowledgements added up, not when the wait ran out.
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("the sender waited %v although every connection had acknowledged", elapsed)
	}

	for name, node := range map[string]*whispered{"two": onTwo, "three": onThree} {
		if node.count() != 1 {
			t.Fatalf("node %s was handed %d frames, want 1", name, node.count())
		}
		if node.users[0] != person {
			t.Errorf("node %s delivered to %v, not the person it was for", name, node.users[0])
		}
		if !bytes.Equal(node.frames[0].Payload, frame.Payload) || node.frames[0].Type != frame.Type {
			t.Errorf("node %s received the frame altered (%d bytes, want %d)",
				name, len(node.frames[0].Payload), len(frame.Payload))
		}
	}
	if onOne.count() != 0 {
		t.Error("the sending node delivered its own frame again")
	}
}

/*
When presence promises more connections than answer, the sender gives up at the
limit and says how many did — a connection that went without closing cleanly
must cost a moment, not hang the program waiting on it.
*/
func TestAWhisperStopsWaitingAtItsLimit(t *testing.T) {
	pool := brokerPool(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodeOne := realtime.NewPostgresBroker(pool.Pool, "node-one", quiet)
	nodeTwo := realtime.NewPostgresBroker(pool.Pool, "node-two", quiet)
	listenPrivately(ctx, nodeOne, &whispered{})
	listenPrivately(ctx, nodeTwo, &whispered{connections: 1})
	time.Sleep(200 * time.Millisecond)

	started := time.Now()
	delivered, err := nodeOne.Whisper(ctx, uuid.New(),
		realtime.Frame{Type: realtime.FramePrivate, Payload: json.RawMessage(`{}`)}, 4, 400*time.Millisecond)
	if err != nil {
		t.Fatalf("whispering: %v", err)
	}
	if delivered != 1 {
		t.Errorf("told %d received it, want the 1 that did", delivered)
	}
	if elapsed := time.Since(started); elapsed < 350*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("waited %v, want about the 400ms limit", elapsed)
	}
}

// A single node has nobody to whisper to, and says so at once.
func TestASingleNodeWhispersToNobody(t *testing.T) {
	delivered, err := realtime.Alone{}.Whisper(context.Background(), uuid.New(), realtime.Frame{}, 3, time.Minute)
	if err != nil || delivered != 0 {
		t.Errorf("Alone whispered to %d (%v), want 0 and no error", delivered, err)
	}
}

/*
A node holding no connection for the person drops their frame unread, and does
not claim to have delivered it.

Every node hears every private frame, so this is what keeps their cost to the
nodes that have somebody to give it to.
*/
func TestANodeWithoutThePersonIgnoresTheirFrame(t *testing.T) {
	pool := brokerPool(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodeOne := realtime.NewPostgresBroker(pool.Pool, "node-one", quiet)
	nodeTwo := realtime.NewPostgresBroker(pool.Pool, "node-two", quiet)
	listenPrivately(ctx, nodeOne, &whispered{})

	elsewhere := &whispered{connections: 1}
	go func() {
		_ = nodeTwo.Listen(ctx, realtime.Receivers{
			Announcement: func(realtime.Announcement) {},
			Private:      elsewhere.deliver,
			Holds:        func(uuid.UUID) bool { return false },
		})
	}()
	time.Sleep(200 * time.Millisecond)

	delivered, err := nodeOne.Whisper(ctx, uuid.New(),
		realtime.Frame{Type: realtime.FramePrivate, Payload: json.RawMessage(`{}`)}, 1, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("whispering: %v", err)
	}
	if delivered != 0 || elsewhere.count() != 0 {
		t.Errorf("a node holding nobody delivered %d and acknowledged %d", elsewhere.count(), delivered)
	}
}

// A piece claiming its frame has more pieces than any frame can is ignored,
// rather than believed and allocated for.
func TestAnImpossiblePieceIsIgnored(t *testing.T) {
	pool := brokerPool(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := realtime.NewPostgresBroker(pool.Pool, "node-two", quiet)
	got := &whispered{connections: 1}
	listenPrivately(ctx, node, got)
	time.Sleep(200 * time.Millisecond)

	whole, _ := json.Marshal(map[string]any{"u": uuid.New(), "f": map[string]string{"type": "private"}})
	forged, _ := json.Marshal(map[string]any{
		"i": uuid.NewString(), "o": "elsewhere", "n": 0, "of": 1 << 30,
		"d": base64.StdEncoding.EncodeToString(whole),
	})
	if _, err := pool.Exec(ctx, "SELECT pg_notify('tdc_private', $1)", string(forged)); err != nil {
		t.Fatalf("notifying: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if got.count() != 0 {
		t.Error("a piece claiming a billion siblings was acted on")
	}
}
