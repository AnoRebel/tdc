package realtime

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// stubMembership lets a test change who is in a room between deliveries.
type stubMembership struct {
	mu      sync.RWMutex
	members map[uuid.UUID][]uuid.UUID
}

func newStubMembership() *stubMembership {
	return &stubMembership{members: make(map[uuid.UUID][]uuid.UUID)}
}

func (s *stubMembership) MemberIDs(_ context.Context, roomID uuid.UUID) ([]uuid.UUID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.members[roomID], nil
}

func (s *stubMembership) set(roomID uuid.UUID, members ...uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members[roomID] = members
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// receive waits briefly for a frame, reporting whether one arrived.
func receive(conn *Connection) (Frame, bool) {
	select {
	case frame := <-conn.Frames():
		return frame, true
	case <-time.After(250 * time.Millisecond):
		return Frame{}, false
	}
}

func TestSubscribedMemberReceivesEvents(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())

	user := uuid.New()
	room := uuid.New()
	membership.set(room, user)

	conn := NewConnection(user)
	conn.Subscribe(room)
	hub.Add(conn)

	hub.Publish(context.Background(), room, Frame{Type: FrameEvent, RoomID: room.String(), Sequence: 1})

	frame, ok := receive(conn)
	if !ok {
		t.Fatal("a subscribed member received nothing")
	}
	if frame.Sequence != 1 {
		t.Errorf("sequence = %d, want 1", frame.Sequence)
	}
}

// A member who has not subscribed to a room must not receive its events, so
// opening one channel does not stream every room they belong to.
func TestUnsubscribedMemberReceivesNothing(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())

	user := uuid.New()
	room := uuid.New()
	membership.set(room, user)

	conn := NewConnection(user)
	hub.Add(conn)

	hub.Publish(context.Background(), room, Frame{Type: FrameEvent, RoomID: room.String()})

	if _, ok := receive(conn); ok {
		t.Error("an unsubscribed connection received a room's events")
	}
}

// Subscribing to a room does not grant access to it: the hub checks
// membership at delivery, so a client cannot read a room by subscribing.
func TestNonMemberReceivesNothingEvenWhenSubscribed(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())

	member := uuid.New()
	outsider := uuid.New()
	room := uuid.New()
	membership.set(room, member)

	conn := NewConnection(outsider)
	conn.Subscribe(room)
	hub.Add(conn)

	hub.Publish(context.Background(), room, Frame{Type: FrameEvent, RoomID: room.String()})

	if _, ok := receive(conn); ok {
		t.Error("a non-member received a room's events by subscribing to it")
	}
}

// The specification's requirement: removal takes effect on a live connection,
// without the client having to reconnect.
func TestRemovedMemberStopsReceivingWithoutReconnecting(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())

	staying := uuid.New()
	leaving := uuid.New()
	room := uuid.New()
	membership.set(room, staying, leaving)

	stayingConn := NewConnection(staying)
	stayingConn.Subscribe(room)
	hub.Add(stayingConn)

	leavingConn := NewConnection(leaving)
	leavingConn.Subscribe(room)
	hub.Add(leavingConn)

	ctx := context.Background()
	hub.Publish(ctx, room, Frame{Type: FrameEvent, RoomID: room.String(), Sequence: 1})

	if _, ok := receive(stayingConn); !ok {
		t.Fatal("the staying member received nothing before the removal")
	}
	if _, ok := receive(leavingConn); !ok {
		t.Fatal("the leaving member received nothing before the removal")
	}

	// Removed from the room. The connection stays open.
	membership.set(room, staying)

	hub.Publish(ctx, room, Frame{Type: FrameEvent, RoomID: room.String(), Sequence: 2})

	if _, ok := receive(stayingConn); !ok {
		t.Error("the remaining member stopped receiving events")
	}
	if _, ok := receive(leavingConn); ok {
		t.Error("a removed member still receives events on an open connection")
	}
}

// A person's several devices all receive an event.
func TestEveryDeviceOfAMemberReceives(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())

	user := uuid.New()
	room := uuid.New()
	membership.set(room, user)

	first := NewConnection(user)
	first.Subscribe(room)
	hub.Add(first)

	second := NewConnection(user)
	second.Subscribe(room)
	hub.Add(second)

	hub.Publish(context.Background(), room, Frame{Type: FrameEvent, RoomID: room.String()})

	if _, ok := receive(first); !ok {
		t.Error("the first device received nothing")
	}
	if _, ok := receive(second); !ok {
		t.Error("the second device received nothing")
	}
}

// Subscriptions change without tearing down the connection.
func TestSubscriptionsChangeInPlace(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())

	user := uuid.New()
	first := uuid.New()
	second := uuid.New()
	membership.set(first, user)
	membership.set(second, user)

	conn := NewConnection(user)
	conn.Subscribe(first)
	hub.Add(conn)

	ctx := context.Background()

	// Adding a room mid-connection starts delivery for it.
	conn.Subscribe(second)
	hub.Publish(ctx, second, Frame{Type: FrameEvent, RoomID: second.String()})
	if _, ok := receive(conn); !ok {
		t.Error("a newly subscribed room delivered nothing")
	}

	// Removing one stops delivery for it, and leaves the other alone.
	conn.Unsubscribe(first)
	hub.Publish(ctx, first, Frame{Type: FrameEvent, RoomID: first.String()})
	if _, ok := receive(conn); ok {
		t.Error("an unsubscribed room still delivers")
	}

	hub.Publish(ctx, second, Frame{Type: FrameEvent, RoomID: second.String()})
	if _, ok := receive(conn); !ok {
		t.Error("unsubscribing from one room stopped delivery for another")
	}
}

// A client that cannot keep up is dropped rather than being allowed to stall
// delivery for everyone else in the room.
func TestSlowClientIsDropped(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())

	slow := uuid.New()
	room := uuid.New()
	membership.set(room, slow)

	conn := NewConnection(slow)
	conn.Subscribe(room)
	hub.Add(conn)

	ctx := context.Background()
	// Publish far more than the buffer holds, without reading any of it.
	for i := range sendBuffer * 2 {
		hub.Publish(ctx, room, Frame{Type: FrameEvent, RoomID: room.String(), Sequence: int64(i)})
	}

	if hub.Count() != 0 {
		t.Error("a client that never reads was not dropped")
	}
}

// Removing a connection closes it, so its writer goroutine ends.
func TestRemoveClosesTheConnection(t *testing.T) {
	hub := NewHub(newStubMembership(), discardLogger())

	conn := NewConnection(uuid.New())
	hub.Add(conn)
	hub.Remove(conn)

	select {
	case <-conn.Closed():
	case <-time.After(time.Second):
		t.Error("removing a connection did not close it")
	}

	if hub.Count() != 0 {
		t.Errorf("hub still holds %d connections", hub.Count())
	}
}

// Closing twice must be safe: a connection can end from either side.
func TestClosingTwiceIsSafe(t *testing.T) {
	conn := NewConnection(uuid.New())
	conn.Close()
	conn.Close()
}

// The hub is shared across every request, so concurrent use must be safe.
func TestConcurrentUseIsSafe(t *testing.T) {
	membership := newStubMembership()
	hub := NewHub(membership, discardLogger())
	ctx := context.Background()

	room := uuid.New()
	var users []uuid.UUID
	for range 10 {
		user := uuid.New()
		users = append(users, user)
	}
	membership.set(room, users...)

	var wait sync.WaitGroup

	for _, user := range users {
		wait.Add(1)
		go func() {
			defer wait.Done()
			conn := NewConnection(user)
			conn.Subscribe(room)
			hub.Add(conn)
			// Drain, so the connection is not dropped for being slow.
			go func() {
				for {
					select {
					case <-conn.Frames():
					case <-conn.Closed():
						return
					}
				}
			}()
		}()
	}

	wait.Add(1)
	go func() {
		defer wait.Done()
		for i := range 50 {
			hub.Publish(ctx, room, Frame{Type: FrameEvent, Sequence: int64(i)})
		}
	}()

	wait.Wait()
	hub.CloseAll()

	if hub.Count() != 0 {
		t.Errorf("hub holds %d connections after CloseAll", hub.Count())
	}
}

func TestCloseAllEndsEveryConnection(t *testing.T) {
	hub := NewHub(newStubMembership(), discardLogger())

	var conns []*Connection
	for range 5 {
		conn := NewConnection(uuid.New())
		hub.Add(conn)
		conns = append(conns, conn)
	}

	hub.CloseAll()

	for i, conn := range conns {
		select {
		case <-conn.Closed():
		case <-time.After(time.Second):
			t.Errorf("connection %d was not closed", i)
		}
	}
}
