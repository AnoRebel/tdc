package notify_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/notify"
)

// recorder stands in for the push service, capturing who was reached.
type recorder struct {
	reached []uuid.UUID
	last    notify.Delivery
}

func (r *recorder) Deliver(_ context.Context, userID uuid.UUID, about notify.Delivery) {
	r.reached = append(r.reached, userID)
	r.last = about
}

/*
The wiring, which is the part that can be complete and still do nothing.

Every piece of push delivery can be written and unit tested — keys, payloads,
subscriptions, the sending itself — and a device can subscribe to a server that
never sends it anything, because nothing calls it. This asserts the call
happens.
*/
func TestARaisedNotificationReachesTheDevices(t *testing.T) {
	pool := testPool(t)
	queries := dbgen.New(pool)
	ctx := context.Background()

	sender := createUser(t, queries, "amina")
	recipient := createUser(t, queries, "juma")
	room := createRoom(t, queries, "general")

	devices := &recorder{}
	service := notify.NewService(pool, quietLogger()).WithPush(devices)

	event := createEvent(t, queries, room, sender.id)
	// Addressed by name, because the default preference is mentions only: an
	// unaddressed message on a channel notifies nobody, by design.
	service.ForMessageIn(ctx, event, "@juma habari", members(recipient), false,
		notify.Delivery{Sender: "Amina", Room: "general"})

	if len(devices.reached) != 1 || devices.reached[0] != recipient.id {
		t.Fatalf("a raised notification reached %v, want the recipient alone", devices.reached)
	}
	if devices.last.Body != "@juma habari" {
		t.Errorf("the delivery carried %q", devices.last.Body)
	}
	if devices.last.EventID != event.ID {
		t.Errorf("the delivery names event %q, not %q", devices.last.EventID, event.ID)
	}
}

/*
Somebody never hears their own message on their own phone.

Checked here as well as in the notification itself because the two are separate
paths now: a rule enforced in one and forgotten in the other is how a person
ends up being buzzed by their own typing.
*/
func TestNobodyIsPushedTheirOwnMessage(t *testing.T) {
	pool := testPool(t)
	queries := dbgen.New(pool)

	sender := createUser(t, queries, "amina")
	room := createRoom(t, queries, "general")

	devices := &recorder{}
	service := notify.NewService(pool, quietLogger()).WithPush(devices)

	event := createEvent(t, queries, room, sender.id)
	// The sender is in the room, as senders are.
	// Addressed to themselves, so only the sender rule can hold it back.
	service.ForMessageIn(context.Background(), event, "@amina habari", members(sender), false,
		notify.Delivery{Sender: "Amina", Room: "general"})

	if len(devices.reached) != 0 {
		t.Errorf("somebody's own message reached their devices: %v", devices.reached)
	}
}

/*
A phone does not light up about the message somebody is watching arrive.

The suppression rules decide when a notification is raised at all, and push
follows that decision rather than making its own. Somebody reading a channel
should not be interrupted by what they are reading — the interruption that
teaches people to switch notifications off.
*/
func TestAWatchedConversationDoesNotReachTheDevices(t *testing.T) {
	pool := testPool(t)
	queries := dbgen.New(pool)

	sender := createUser(t, queries, "amina")
	recipient := createUser(t, queries, "juma")
	room := createRoom(t, queries, "general")

	devices := &recorder{}
	service := notify.NewService(pool, quietLogger()).
		WithSuppression(watchingEverything{}, nil).
		WithPush(devices)

	event := createEvent(t, queries, room, sender.id)
	service.ForMessageIn(context.Background(), event, "@juma habari", members(recipient), false,
		notify.Delivery{Sender: "Amina", Room: "general"})

	if len(devices.reached) != 0 {
		t.Errorf("a watched conversation still reached devices: %v", devices.reached)
	}
}

// watchingEverything says somebody is looking at whatever they are asked about.
type watchingEverything struct{}

func (watchingEverything) IsWatching(uuid.UUID, uuid.UUID) bool { return true }

// A server with no push configured still notifies, which is what makes leaving
// it unset an ordinary way to run rather than a broken one.
func TestNotificationsAreRaisedWithoutPush(t *testing.T) {
	pool := testPool(t)
	queries := dbgen.New(pool)
	ctx := context.Background()

	sender := createUser(t, queries, "amina")
	recipient := createUser(t, queries, "juma")
	room := createRoom(t, queries, "general")

	service := notify.NewService(pool, quietLogger())

	event := createEvent(t, queries, room, sender.id)
	service.ForMessage(ctx, event, "@juma habari", members(recipient), false)

	count, err := service.UnreadCount(ctx, recipient.id)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 1 {
		t.Errorf("a server without push raised %d notifications, want 1", count)
	}
}
