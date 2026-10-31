package notify_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testPool opens a database for one test, cleaned before and after.
func testPool(t *testing.T) *db.Pool {
	t.Helper()

	url := dbtest.URL(t, "notify")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.Migrate(ctx, url, quietLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pool, err := db.Open(ctx, db.Config{URL: url}, quietLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		// Rooms and users cascade to events, memberships and notifications.
		for _, statement := range []string{"DELETE FROM rooms", "DELETE FROM users"} {
			if _, err := pool.Exec(cleanCtx, statement); err != nil {
				t.Errorf("clean: %v", err)
			}
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	return pool
}

func createUser(t *testing.T, queries *dbgen.Queries, username string) person {
	t.Helper()

	user, err := queries.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       username,
		UsernameFolded: username,
		DisplayName:    username,
	})
	if err != nil {
		t.Fatalf("CreateUser %q: %v", username, err)
	}
	return person{id: user.ID, username: username}
}

func createRoom(t *testing.T, queries *dbgen.Queries, name string) uuid.UUID {
	t.Helper()

	slug := name
	room, err := queries.CreateRoom(context.Background(), dbgen.CreateRoomParams{
		ID:         uuid.New(),
		Slug:       &slug,
		SlugFolded: &slug,
		Name:       name,
		Kind:       "channel",
		Visibility: "public",
		HomeServer: "tdc.test",
	})
	if err != nil {
		t.Fatalf("CreateRoom %q: %v", name, err)
	}
	return room.ID
}

func createEvent(t *testing.T, queries *dbgen.Queries, room, sender uuid.UUID) dbgen.Event {
	t.Helper()

	event, err := queries.AppendEvent(context.Background(), dbgen.AppendEventParams{
		ID:            uuid.New().String(),
		RoomID:        room,
		Sequence:      1,
		SenderID:      &sender,
		SenderAddress: "someone@tdc.test",
		RoomAddress:   "general@tdc.test",
		OriginServer:  "tdc.test",
		Type:          "m.message",
		Content:       []byte(`{"body":"Habari"}`),
		CreatedAt:     time.Now().UnixMilli(),
		Nonce:         "nonce",
		Signature:     "signature",
		KeyID:         "test-key",
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	return event
}

/*
members is the recipient list ForMessage takes.

Usernames are carried through because a mention is matched against them, and
the default preference notifies on mentions alone.
*/
func members(people ...person) []dbgen.ListRoomMembersRow {
	rows := make([]dbgen.ListRoomMembersRow, 0, len(people))
	for _, who := range people {
		rows = append(rows, dbgen.ListRoomMembersRow{ID: who.id, Username: who.username})
	}
	return rows
}

// person is an account in a test, kept together with its name so a mention can
// be aimed at it.
type person struct {
	id       uuid.UUID
	username string
}
