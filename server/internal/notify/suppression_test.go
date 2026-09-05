package notify_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/notify"
)

// watching is a stand-in for the live connections.
type watching struct {
	rooms map[uuid.UUID]uuid.UUID
}

func (w watching) IsWatching(userID, roomID uuid.UUID) bool {
	return w.rooms[userID] == roomID
}

// silencing is a stand-in for presence.
type silencing struct {
	quiet map[uuid.UUID]bool
}

func (s silencing) SuppressesNotifications(_ context.Context, userID uuid.UUID) bool {
	return s.quiet[userID]
}

/*
TestSuppressionIsOptional is what keeps a minimal server working.

Neither the live connections nor presence is required. A service with neither
must notify rather than fail, since both are refinements on top of a
notification that was already wanted.
*/
func TestSuppressionIsOptional(t *testing.T) {
	// A service with nothing wired: the zero value of both interfaces is nil,
	// and calling through a nil interface would panic if this were wrong.
	service := &notify.Service{}

	if notify.SuppressedForTest(service, context.Background(), uuid.New(), uuid.New()) {
		t.Error("a service with no suppression wired suppressed a notification")
	}
}

/*
TestWatchingSuppresses is the rule 6.3 exists for.

A notification about a message somebody just watched arrive is the interruption
that teaches people to switch notifications off entirely.
*/
func TestWatchingSuppresses(t *testing.T) {
	reader := uuid.New()
	room := uuid.New()

	service := (&notify.Service{}).WithSuppression(
		watching{rooms: map[uuid.UUID]uuid.UUID{reader: room}}, nil)

	if !notify.SuppressedForTest(service, context.Background(), reader, room) {
		t.Error("somebody watching a conversation was still notified about it")
	}

	// A different room is not suppressed: watching one channel says nothing
	// about the others.
	if notify.SuppressedForTest(service, context.Background(), reader, uuid.New()) {
		t.Error("watching one conversation suppressed another")
	}

	// And somebody else watching does not silence this person.
	if notify.SuppressedForTest(service, context.Background(), uuid.New(), room) {
		t.Error("one person watching suppressed another person's notification")
	}
}

// TestStatusSuppresses covers the operator-defined flag, which until now was a
// column nothing consulted.
func TestStatusSuppresses(t *testing.T) {
	quiet := uuid.New()
	available := uuid.New()

	service := (&notify.Service{}).WithSuppression(
		nil, silencing{quiet: map[uuid.UUID]bool{quiet: true}})

	room := uuid.New()

	if !notify.SuppressedForTest(service, context.Background(), quiet, room) {
		t.Error("a status that suppresses notifications did not")
	}
	if notify.SuppressedForTest(service, context.Background(), available, room) {
		t.Error("a status that does not suppress notifications did")
	}
}
