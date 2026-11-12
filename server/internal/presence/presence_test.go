package presence_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/presence"
)

func newTestService(t *testing.T) (*presence.Service, *dbgen.Queries, *pgxpool.Pool) {
	t.Helper()

	url := dbtest.URL(t, "presence")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, db.Config{URL: url}, dbtest.Logger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
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

	queries := dbgen.New(pool)
	return presence.NewService(queries), queries, pool.Pool
}

func makeUser(t *testing.T, queries *dbgen.Queries, name string) uuid.UUID {
	t.Helper()
	user, err := queries.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID: uuid.New(), Username: name, UsernameFolded: name, DisplayName: name,
	})
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	return user.ID
}

func TestConnectingMakesSomeonePresent(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	before, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if before.State != presence.StateOffline {
		t.Errorf("state = %q before connecting, want offline", before.State)
	}

	if err := service.Connected(ctx, user); err != nil {
		t.Fatalf("Connected: %v", err)
	}

	after, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if after.State != presence.StateOnline {
		t.Errorf("state = %q after connecting, want online", after.State)
	}
}

// Presence ends when the last connection closes, not the first: closing a
// laptop while a phone stays connected must not make someone appear offline.
func TestPresenceSurvivesClosingOneOfSeveralConnections(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	for range 2 {
		if err := service.Connected(ctx, user); err != nil {
			t.Fatalf("Connected: %v", err)
		}
	}

	if err := service.Disconnected(ctx, user); err != nil {
		t.Fatalf("Disconnected: %v", err)
	}

	still, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if still.State != presence.StateOnline {
		t.Error("closing one of two connections made the person appear offline")
	}

	if err := service.Disconnected(ctx, user); err != nil {
		t.Fatalf("Disconnected: %v", err)
	}

	gone, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if gone.State != presence.StateOffline {
		t.Error("closing the last connection left the person appearing online")
	}
}

// A connection that ended without a clean close must expire rather than
// leaving someone permanently present.
func TestStalePresenceExpires(t *testing.T) {
	service, queries, pool := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	if err := service.Connected(ctx, user); err != nil {
		t.Fatalf("Connected: %v", err)
	}

	// Age the connection past the staleness threshold, as an ungraceful
	// disconnection would.
	if _, err := pool.Exec(ctx,
		"UPDATE user_presence SET last_seen_at = NOW() - INTERVAL '10 minutes' WHERE user_id = $1",
		user,
	); err != nil {
		t.Fatalf("age presence: %v", err)
	}

	// Reported offline immediately, before any sweep has run.
	stale, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if stale.State != presence.StateOffline {
		t.Error("a connection that stopped being refreshed still reads as online")
	}

	// And the sweep clears the count, so it does not accumulate.
	if err := service.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	record, err := queries.GetPresence(ctx, user)
	if err != nil {
		t.Fatalf("GetPresence: %v", err)
	}
	if record.Connections != 0 {
		t.Errorf("connections = %d after the sweep, want 0", record.Connections)
	}
}

// Appearing offline must be honoured while the person stays fully functional.
func TestInvisibilityIsHonoured(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	if err := service.Connected(ctx, user); err != nil {
		t.Fatalf("Connected: %v", err)
	}
	if err := service.SetStatus(ctx, user, "busy", "in a meeting", nil); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if err := service.SetInvisible(ctx, user, true); err != nil {
		t.Fatalf("SetInvisible: %v", err)
	}

	hidden, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if hidden.State != presence.StateOffline {
		t.Error("someone who chose to appear offline is shown as online")
	}
	// The status is withheld too: showing "in a meeting" would give away that
	// they are there.
	if hidden.StatusText != "" {
		t.Errorf("statusText = %q while invisible, want it withheld", hidden.StatusText)
	}
}

func TestStatusMustBeOneTheOperatorConfigured(t *testing.T) {
	service, queries, _ := newTestService(t)
	user := makeUser(t, queries, "amina")

	if err := service.SetStatus(context.Background(), user, "inventing-my-own", "", nil); !errors.Is(err, presence.ErrUnknownStatus) {
		t.Errorf("error = %v, want ErrUnknownStatus", err)
	}
}

// A status with an expiry reverts on its own.
func TestStatusExpires(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	past := time.Now().Add(-time.Minute)
	if err := service.SetStatus(ctx, user, "busy", "back soon", &past); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	// Reported as the default immediately, before the sweep runs, so a reader
	// never sees a status that has lapsed.
	lapsed, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if lapsed.Status != "available" {
		t.Errorf("status = %q after expiry, want available", lapsed.Status)
	}
}

// Whether a status withholds notifications comes from the operator's
// configuration, not from the client.
func TestStatusCarriesItsNotificationBehaviour(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	if err := service.SetStatus(ctx, user, "busy", "", nil); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	busy, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if !busy.SuppressesNotifications {
		t.Error("the busy status does not withhold notifications")
	}

	if err := service.SetStatus(ctx, user, "available", "", nil); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	available, err := service.Of(ctx, user)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if available.SuppressesNotifications {
		t.Error("the available status withholds notifications")
	}
}

func TestStatusesAreOperatorConfigured(t *testing.T) {
	service, _, _ := newTestService(t)

	statuses, err := service.Statuses(context.Background())
	if err != nil {
		t.Fatalf("Statuses: %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("a server offers no statuses at all")
	}

	byKey := map[string]bool{}
	for _, status := range statuses {
		byKey[status.Key] = true
	}
	for _, expected := range []string{"available", "busy", "away"} {
		if !byKey[expected] {
			t.Errorf("the built-in status %q is missing", expected)
		}
	}
}

// A typing indication expires on its own, so someone who stops mid-sentence
// does not leave it showing forever.
func TestTypingIndicationExpires(t *testing.T) {
	service, queries, pool := newTestService(t)
	ctx := context.Background()

	typist := makeUser(t, queries, "amina")
	watcher := makeUser(t, queries, "juma")
	room := makeRoom(t, queries, typist)

	if err := service.StartTyping(ctx, room, typist); err != nil {
		t.Fatalf("StartTyping: %v", err)
	}

	typing, err := service.Typing(ctx, room, watcher)
	if err != nil {
		t.Fatalf("Typing: %v", err)
	}
	if len(typing) != 1 {
		t.Fatalf("%d people typing, want 1", len(typing))
	}

	// Age the indication past its lifetime, as a client that went away would.
	if _, err := pool.Exec(ctx,
		"UPDATE typing_indicators SET expires_at = NOW() - INTERVAL '1 minute' WHERE user_id = $1",
		typist,
	); err != nil {
		t.Fatalf("age indication: %v", err)
	}

	expired, err := service.Typing(ctx, room, watcher)
	if err != nil {
		t.Fatalf("Typing: %v", err)
	}
	if len(expired) != 0 {
		t.Error("an expired typing indication is still shown")
	}
}

// Nobody needs to be told that they themselves are typing.
func TestTypingExcludesTheAsker(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	typist := makeUser(t, queries, "amina")
	room := makeRoom(t, queries, typist)

	if err := service.StartTyping(ctx, room, typist); err != nil {
		t.Fatalf("StartTyping: %v", err)
	}

	own, err := service.Typing(ctx, room, typist)
	if err != nil {
		t.Fatalf("Typing: %v", err)
	}
	if len(own) != 0 {
		t.Error("a person is shown their own typing indication")
	}
}

// Opting out means nothing is sent on that person's behalf.
func TestTypingCanBeDisabled(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	typist := makeUser(t, queries, "amina")
	watcher := makeUser(t, queries, "juma")
	room := makeRoom(t, queries, typist)

	if err := service.SetTypingPreference(ctx, typist, false); err != nil {
		t.Fatalf("SetTypingPreference: %v", err)
	}
	if err := service.StartTyping(ctx, room, typist); err != nil {
		t.Fatalf("StartTyping: %v", err)
	}

	typing, err := service.Typing(ctx, room, watcher)
	if err != nil {
		t.Fatalf("Typing: %v", err)
	}
	if len(typing) != 0 {
		t.Error("an indication was sent for someone who opted out")
	}
}

func TestStopTypingWithdrawsEarly(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	typist := makeUser(t, queries, "amina")
	watcher := makeUser(t, queries, "juma")
	room := makeRoom(t, queries, typist)

	if err := service.StartTyping(ctx, room, typist); err != nil {
		t.Fatalf("StartTyping: %v", err)
	}
	if err := service.StopTyping(ctx, room, typist); err != nil {
		t.Fatalf("StopTyping: %v", err)
	}

	typing, err := service.Typing(ctx, room, watcher)
	if err != nil {
		t.Fatalf("Typing: %v", err)
	}
	if len(typing) != 0 {
		t.Error("an indication survived being withdrawn")
	}
}

// makeRoom creates a room directly, since this package tests presence rather
// than the rooms service.
func makeRoom(t *testing.T, queries *dbgen.Queries, creator uuid.UUID) uuid.UUID {
	t.Helper()

	slug := "room-" + uuid.New().String()[:8]
	room, err := queries.CreateRoom(context.Background(), dbgen.CreateRoomParams{
		ID:         uuid.New(),
		Slug:       &slug,
		SlugFolded: &slug,
		Name:       slug,
		Kind:       "channel",
		Visibility: "public",
		HomeServer: "tdc.test",
		CreatedBy:  &creator,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	return room.ID
}

/*
Appearing offline hides a status from everybody else; it does not undo it.

Somebody who is busy and also appears offline is still not to be interrupted,
and still sees their own choice — both were lost when the person's own view
went through what others are shown.
*/
func TestAppearingOfflineKeepsYourOwnStatus(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	later := time.Now().Add(time.Hour)
	if err := service.SetStatus(ctx, user, "busy", "in a meeting", &later); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if err := service.SetInvisible(ctx, user, true); err != nil {
		t.Fatalf("SetInvisible: %v", err)
	}

	if !service.SuppressesNotifications(ctx, user) {
		t.Error("busy stopped withholding notifications once the person appeared offline")
	}

	own, err := service.Own(ctx, user)
	if err != nil {
		t.Fatalf("Own: %v", err)
	}
	if own.Status != "busy" || own.StatusText != "in a meeting" || !own.Invisible {
		t.Errorf("own = %+v, want busy, its note, and invisible", own)
	}
	if own.ExpiresAt == nil || own.ExpiresAt.Sub(later).Abs() > time.Millisecond {
		t.Errorf("expiresAt = %v, want %v", own.ExpiresAt, later)
	}
}

// A lapsed status is not shown to its owner either.
func TestOwnStatusLapses(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()
	user := makeUser(t, queries, "amina")

	past := time.Now().Add(-time.Minute)
	if err := service.SetStatus(ctx, user, "away", "lunch", &past); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	own, err := service.Own(ctx, user)
	if err != nil {
		t.Fatalf("Own: %v", err)
	}
	if own.Status != "available" || own.StatusText != "" || own.ExpiresAt != nil {
		t.Errorf("own = %+v after expiry, want the default with no note or expiry", own)
	}
}
