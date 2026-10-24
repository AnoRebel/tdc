package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
Whatever hears of an event's log entry can already read the event.

Webhooks are queued from that hook, and look up the event's channel when they
are: told inside the transaction, they found no event and queued nothing,
which under load lost an event's webhooks now and then. Sabotage-verified:
telling from AppendIn again fails this every time.
*/
func TestAnEventIsToldOfOnlyOnceItIsCommitted(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{URL: dbtest.URL(t, "calendartold")}, dbtest.Logger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	queries := dbgen.New(pool)

	key, err := events.NewKeyPair("told-key")
	if err != nil {
		t.Fatal(err)
	}
	roomsService := rooms.NewService(queries, events.NewKeyring(key), "told.test", dbtest.Logger())
	var told, visible int
	roomsService.OnAppended(func(ctx context.Context, entry dbgen.Event) {
		told++
		if _, err := queries.GetCalendarEvent(ctx, entry.RoomID); err == nil {
			visible++
		}
	})

	username := "told" + uuid.NewString()[:8]
	actor, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		ID: uuid.New(), Username: username, UsernameFolded: username, DisplayName: username,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", actor.ID) })

	viewer := Viewer{UserID: actor.ID, Permissions: authz.Resolve([]authz.Grant{
		{Permission: authz.PermEventCreate, Granted: true},
	})}
	starts := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	created, err := NewService(pool, roomsService, dbtest.Logger()).Create(ctx, actor, viewer, Draft{
		Title: "Meetup", StartsAt: starts, EndsAt: starts.Add(time.Hour), TimeZone: "Africa/Dar_es_Salaam",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM rooms WHERE id = $1", created.Event.ID) })

	if told == 0 || visible != told {
		t.Errorf("told of %d entries, %d of them before their event could be read", told, told-visible)
	}
}
