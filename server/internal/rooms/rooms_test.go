package rooms_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

const testServerName = "tdc.test"

func newTestService(t *testing.T) (*rooms.Service, *dbgen.Queries, *pgxpool.Pool) {
	t.Helper()

	url := dbtest.URL(t, "rooms")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		// Events, members and sequences all cascade from rooms and users.
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

	key, err := events.NewKeyPair("test-key")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}

	queries := dbgen.New(pool)
	service := rooms.NewService(queries, events.NewKeyring(key), testServerName, logger)
	return service, queries, pool.Pool
}

func makeUser(t *testing.T, queries *dbgen.Queries, name string) dbgen.User {
	t.Helper()
	user, err := queries.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       name,
		UsernameFolded: name,
		DisplayName:    name,
	})
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	return user
}

func makeRoom(t *testing.T, service *rooms.Service, creator uuid.UUID, slug string) dbgen.Room {
	t.Helper()
	room, err := service.Create(context.Background(), rooms.NewRoom{
		Slug:      slug,
		Name:      slug,
		CreatedBy: creator,
	})
	if err != nil {
		t.Fatalf("create room %q: %v", slug, err)
	}
	return room
}

func bodyOf(t *testing.T, event dbgen.Event) string {
	t.Helper()
	var content struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(event.Content, &content); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	return content.Body
}

func TestCreateRoomRecordsItAndJoinsTheCreator(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	creator := makeUser(t, queries, "amina")
	room := makeRoom(t, service, creator.ID, "general")

	if room.HomeServer != testServerName {
		t.Errorf("homeServer = %q, want this server", room.HomeServer)
	}

	// The creator must be a member: a room nobody is in cannot be administered.
	member, err := queries.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: room.ID, UserID: creator.ID,
	})
	if err != nil {
		t.Fatalf("IsRoomMember: %v", err)
	}
	if !member {
		t.Error("the creator is not a member of the room they made")
	}

	// Creation is itself an event, so the log is complete from the start.
	timeline, err := service.Timeline(ctx, room.ID, creator.ID, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(timeline) != 1 {
		t.Fatalf("timeline has %d events, want the creation event", len(timeline))
	}
	if timeline[0].Type != string(events.TypeRoomCreated) {
		t.Errorf("first event is %q, want %q", timeline[0].Type, events.TypeRoomCreated)
	}
}

// Addresses name the home server, so they stay unambiguous once servers
// exchange events.
func TestEventsCarryFederatedAddresses(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "juma")
	room := makeRoom(t, service, sender.ID, "general")

	event, err := service.SendMessage(ctx, room.ID, sender.ID, "hello", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if want := "juma@" + testServerName; event.SenderAddress != want {
		t.Errorf("senderAddress = %q, want %q", event.SenderAddress, want)
	}
	if want := "general@" + testServerName; event.RoomAddress != want {
		t.Errorf("roomAddress = %q, want %q", event.RoomAddress, want)
	}
	if event.OriginServer != testServerName {
		t.Errorf("originServer = %q, want %q", event.OriginServer, testServerName)
	}
}

func TestSlugMustBeUnique(t *testing.T) {
	service, queries, _ := newTestService(t)
	creator := makeUser(t, queries, "amina")
	makeRoom(t, service, creator.ID, "general")

	_, err := service.Create(context.Background(), rooms.NewRoom{
		Slug: "GENERAL", Name: "General", CreatedBy: creator.ID,
	})
	if !errors.Is(err, rooms.ErrSlugTaken) {
		t.Errorf("error = %v, want ErrSlugTaken for a differently-cased slug", err)
	}
}

// Every event in a room must have a distinct position, even when senders post
// at the same moment.
func TestConcurrentSendersGetDistinctSequences(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	creator := makeUser(t, queries, "amina")
	room := makeRoom(t, service, creator.ID, "general")

	const senders = 8
	const each = 5

	var wait sync.WaitGroup
	errs := make(chan error, senders*each)

	for worker := range senders {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for message := range each {
				if _, err := service.SendMessage(ctx, room.ID, creator.ID,
					"message from worker", ""); err != nil {
					errs <- err
					_ = worker
					_ = message
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("SendMessage under concurrency: %v", err)
	}

	timeline, err := service.Timeline(ctx, room.ID, creator.ID, 200)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}

	// One creation event plus every message, each at its own position.
	want := senders*each + 1
	if len(timeline) != want {
		t.Fatalf("timeline has %d events, want %d", len(timeline), want)
	}

	seen := map[int64]bool{}
	for _, event := range timeline {
		if seen[event.Sequence] {
			t.Fatalf("sequence %d was allocated twice", event.Sequence)
		}
		seen[event.Sequence] = true
	}
}

// Two readers must observe the same order for the same range.
func TestOrderIsStableAcrossReaders(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	creator := makeUser(t, queries, "amina")
	room := makeRoom(t, service, creator.ID, "general")

	for range 20 {
		if _, err := service.SendMessage(ctx, room.ID, creator.ID, "a message", ""); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	}

	first, err := service.Timeline(ctx, room.ID, creator.ID, 100)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	second, err := service.Timeline(ctx, room.ID, creator.ID, 100)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("two reads returned %d and %d events", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID || first[i].Sequence != second[i].Sequence {
			t.Fatalf("readers disagree at position %d", i)
		}
	}

	// Positions must ascend, which is what makes a gap detectable.
	for i := 1; i < len(first); i++ {
		if first[i].Sequence <= first[i-1].Sequence {
			t.Fatalf("sequence did not ascend: %d then %d",
				first[i-1].Sequence, first[i].Sequence)
		}
	}
}

// A client that retries a submission whose outcome it did not see must not
// create a second message.
func TestRetryWithSameTokenYieldsOneEvent(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "general")

	const token = "client-token-1"

	first, err := service.SendMessage(ctx, room.ID, sender.ID, "only once", token)
	if err != nil {
		t.Fatalf("first SendMessage: %v", err)
	}
	second, err := service.SendMessage(ctx, room.ID, sender.ID, "only once", token)
	if err != nil {
		t.Fatalf("retry SendMessage: %v", err)
	}

	if first.ID != second.ID {
		t.Error("a retry created a second event")
	}
	if first.Sequence != second.Sequence {
		t.Error("a retry allocated a second position")
	}

	timeline, err := service.Timeline(ctx, room.ID, sender.ID, 100)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	// Creation plus exactly one message.
	if len(timeline) != 2 {
		t.Errorf("timeline has %d events, want 2", len(timeline))
	}
}

// One client's token must not suppress another's message.
func TestIdempotencyIsScopedToTheSender(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	first := makeUser(t, queries, "amina")
	second := makeUser(t, queries, "juma")
	room := makeRoom(t, service, first.ID, "general")

	if err := queries.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
		RoomID: room.ID, UserID: second.ID,
	}); err != nil {
		t.Fatalf("AddRoomMember: %v", err)
	}

	const token = "a-token-both-happen-to-use"

	fromFirst, err := service.SendMessage(ctx, room.ID, first.ID, "from amina", token)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	fromSecond, err := service.SendMessage(ctx, room.ID, second.ID, "from juma", token)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if fromFirst.ID == fromSecond.ID {
		t.Error("one sender's token suppressed another sender's message")
	}
	if bodyOf(t, fromSecond) != "from juma" {
		t.Error("the second sender's message was replaced by the first's")
	}
}

func TestEmptyAndOverlongMessagesAreRefused(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "general")

	if _, err := service.SendMessage(ctx, room.ID, sender.ID, "   ", ""); !errors.Is(err, rooms.ErrEmptyMessage) {
		t.Errorf("whitespace-only message error = %v, want ErrEmptyMessage", err)
	}

	overlong := make([]byte, rooms.MaxMessageLength+1)
	for i := range overlong {
		overlong[i] = 'a'
	}
	if _, err := service.SendMessage(ctx, room.ID, sender.ID, string(overlong), ""); !errors.Is(err, rooms.ErrMessageTooLong) {
		t.Errorf("overlong message error = %v, want ErrMessageTooLong", err)
	}
}
