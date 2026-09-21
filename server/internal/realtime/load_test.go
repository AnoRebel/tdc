package realtime

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

/*
What fanout costs when a channel is busy, measured rather than assumed.

The design commits to a server a Tanzanian developer can host on a cheap VPS,
and the number that decides whether that holds is not memory — loadprofile
measures that — but what happens when somebody posts into a channel everybody
is watching. One message becomes one delivery per connected member, and that
multiplication is the whole shape of the problem.

Deliberately reporting rather than failing, for the same reason as the memory
profile: latency moves for legitimate reasons on a shared CI runner, and a job
that failed on a threshold would be routed around within a month. What it does
instead is put the figure and its distribution in front of whoever is
reviewing, so a regression is something a person chose rather than something
that happened.

What it does assert is the part that is a bug rather than a measurement: that
every entitled connection receives every message. A fanout that got faster by
dropping deliveries would otherwise look like an improvement.
*/

// The shape of a busy channel in a community this size: enough connections
// that the fanout multiplication is real, few enough to finish in CI.
const (
	loadConnections = 250
	loadMessages    = 40
)

/*
TestFanoutUnderLoad measures delivery to a channel everybody is watching.

Every connection is drained by a reader of its own, because the alternative
measures the wrong thing entirely: the send buffer holds 64 frames and a
connection that fills it is dropped, so a test that did not read would be
measuring how fast the hub discards clients.
*/
func TestFanoutUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load profile skipped in short mode")
	}

	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())
	defer hub.CloseAll()

	roomID := uuid.New()

	members := make([]uuid.UUID, 0, loadConnections)
	connections := make([]*Connection, 0, loadConnections)
	for range loadConnections {
		userID := uuid.New()
		members = append(members, userID)

		conn := NewConnection(userID)
		conn.Subscribe(roomID)
		hub.Add(conn)
		connections = append(connections, conn)
	}
	membership.set(roomID, members...)

	if got := hub.Count(); got != loadConnections {
		t.Fatalf("hub holds %d connections, want %d", got, loadConnections)
	}

	/*
		A reader per connection, started before anything is published.

		Each records when a frame arrived. The interesting figure is not the
		average — an average hides the client that waited — but the slowest,
		which is what somebody actually experiences when a channel is busy.
	*/
	var received atomic.Int64
	latencies := make([][]time.Duration, loadConnections)

	/*
		When each message was published, read by the readers to date what they
		received. The frame's payload is raw JSON and encoding a timestamp into
		it would measure the encoder as much as the fanout, so the times live
		here and the frame carries only its sequence.
	*/
	sentAt := make([]time.Time, loadMessages)

	var readers sync.WaitGroup
	done := make(chan struct{})
	for i, conn := range connections {
		readers.Add(1)
		go func(index int, conn *Connection) {
			defer readers.Done()
			mine := make([]time.Duration, 0, loadMessages)
			for {
				select {
				case frame := <-conn.Frames():
					if frame.Sequence >= 1 && int(frame.Sequence) <= loadMessages {
						mine = append(mine, time.Since(sentAt[frame.Sequence-1]))
					}
					received.Add(1)
					if len(mine) == loadMessages {
						latencies[index] = mine
						return
					}
				case <-done:
					latencies[index] = mine
					return
				}
			}
		}(i, conn)
	}

	ctx := context.Background()
	publishStarted := time.Now()
	for sequence := range loadMessages {
		sentAt[sequence] = time.Now()
		hub.Publish(ctx, roomID, Frame{
			Type:     "event",
			RoomID:   roomID.String(),
			Sequence: int64(sequence + 1),
		})
	}
	publishTook := time.Since(publishStarted)

	/*
		Everything should already be delivered, so this bounds a hang rather
		than waiting for a duration the measurement depends on.

		Short, and it does not fail on its own: a fanout that drops deliveries
		leaves readers waiting for frames that will never come, and the useful
		report is the count below — "delivered 5000, want 10000" names the bug,
		where "did not finish in thirty seconds" only says something is wrong.
	*/
	waitFor(&readers, 5*time.Second, done)

	/*
		Every entitled connection received every message.

		This is the assertion rather than the measurement. Fanout that dropped
		deliveries would report a smaller latency and be worse, and a figure
		that improves because less work happened is the failure this exists to
		catch.
	*/
	want := int64(loadConnections * loadMessages)
	if got := received.Load(); got != want {
		t.Errorf("delivered %d frames, want %d — fanout dropped %d", got, want, want-got)
	}

	all := make([]time.Duration, 0, want)
	for _, mine := range latencies {
		all = append(all, mine...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	if len(all) == 0 {
		t.Fatal("no latencies recorded, so nothing was measured")
	}

	t.Logf("fanout to %d connections, %d messages (%d deliveries)",
		loadConnections, loadMessages, len(all))
	t.Logf("  publishing took       %s (%s per message)",
		publishTook.Round(time.Millisecond),
		(publishTook / loadMessages).Round(time.Microsecond))
	t.Logf("  delivery latency p50  %s", percentile(all, 0.50).Round(time.Microsecond))
	t.Logf("  delivery latency p95  %s", percentile(all, 0.95).Round(time.Microsecond))
	t.Logf("  delivery latency p99  %s", percentile(all, 0.99).Round(time.Microsecond))
	t.Logf("  slowest delivery      %s", all[len(all)-1].Round(time.Microsecond))
}

/*
TestConnectionsScale reports what holding many connections costs.

Separate from the fanout measurement because they answer different questions: a
server may hold connections cheaply and still deliver slowly, or the reverse.
*/
func TestConnectionsScale(t *testing.T) {
	if testing.Short() {
		t.Skip("load profile skipped in short mode")
	}

	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())
	defer hub.CloseAll()

	roomID := uuid.New()
	members := make([]uuid.UUID, 0, loadConnections)

	started := time.Now()
	for range loadConnections {
		userID := uuid.New()
		members = append(members, userID)
		conn := NewConnection(userID)
		conn.Subscribe(roomID)
		hub.Add(conn)
	}
	adding := time.Since(started)
	membership.set(roomID, members...)

	if got := hub.Count(); got != loadConnections {
		t.Fatalf("hub holds %d connections, want %d", got, loadConnections)
	}

	/*
		A publish with nobody reading, which is the pathological case: every
		connection's buffer fills and the hub has to decide what to do about
		it. What it must not do is block — one client that stopped reading
		cannot be allowed to stall the channel for everybody else.
	*/
	blocked := make(chan time.Duration, 1)
	go func() {
		at := time.Now()
		for range sendBuffer + 8 {
			hub.Publish(context.Background(), roomID, Frame{Type: "event", RoomID: roomID.String()})
		}
		blocked <- time.Since(at)
	}()

	select {
	case took := <-blocked:
		t.Logf("holding %d connections: added in %s, %d publishes to full buffers in %s",
			loadConnections, adding.Round(time.Millisecond), sendBuffer+8, took.Round(time.Millisecond))
	case <-time.After(20 * time.Second):
		t.Fatal("publishing blocked on clients that stopped reading, which stalls the whole channel")
	}
}

/*
waitFor stops the readers once everything has arrived, or once it is clear
nothing more will.

It does not fail on the timeout. Readers still waiting mean frames that never
came, and the caller's count says how many — which is the sentence worth
reading.
*/
func waitFor(group *sync.WaitGroup, limit time.Duration, done chan struct{}) {
	finished := make(chan struct{})
	go func() {
		group.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(limit):
		close(done)
		group.Wait()
	}
}

// percentile returns the value at a fraction through a sorted slice.
func percentile(sorted []time.Duration, fraction float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * fraction)
	return sorted[index]
}
