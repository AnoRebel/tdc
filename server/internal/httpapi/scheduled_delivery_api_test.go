package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
)

/*
Scheduled messages, as sent: an ordinary message, late.

Delivery used to run in the command, beside the server rather than in it, so a
due message was stored and nothing else happened — nobody reading saw it arrive,
nobody mentioned in it heard, and the checks a message passes when somebody
types it were skipped. These make a pass when they choose rather than wait out
the interval, having first made what they scheduled due.
*/

// schedule writes a message to be sent in an hour, returning its identifier.
func schedule(t *testing.T, handler http.Handler, token, roomID, body string) string {
	t.Helper()
	scheduled := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
		map[string]any{"body": body, "sendAt": whenSoon()}, token, http.StatusCreated))
	id, _ := scheduled["id"].(string)
	return id
}

// scheduledDatabase opens the database the harness serves from, for making a
// message due and for reading what became of it.
func scheduledDatabase(t *testing.T) *db.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, db.Config{URL: dbtest.URL(t, "httpapi")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// makeDue moves every outstanding scheduled message to a moment ago.
func makeDue(t *testing.T, pool *db.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE scheduled_messages SET send_at = now() - interval '1 second'
		WHERE sent_event_id IS NULL AND failed_reason = ''`); err != nil {
		t.Fatalf("making scheduled messages due: %v", err)
	}
}

func serverOf(handler http.Handler) *httpapi.Server {
	return handler.(interface{ Server() *httpapi.Server }).Server()
}

// A due message reaches the people reading the channel as it is sent, and
// whoever it mentions is told.
func TestAScheduledMessageArrivesLikeAnyOther(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	pool := scheduledDatabase(t)

	room := makeRoom(t, handler, admin, "general", "public")
	writer := registerAndSignIn(t, handler, "amina")
	reader := registerAndSignIn(t, handler, "baraka")
	for _, person := range []string{writer, reader} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, person, http.StatusOK)
	}

	schedule(t, handler, writer, room, "@baraka the meetup is at six")

	live := httptest.NewServer(handler)
	defer live.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(live.URL, "http")+"/api/live",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + reader}}})
	if err != nil {
		t.Fatalf("opening the live connection: %v", err)
	}
	defer func() { _ = socket.Close(websocket.StatusNormalClosure, "") }()

	for subscribed := false; !subscribed; {
		_, data, err := socket.Read(ctx)
		if err != nil {
			t.Fatalf("the connection ended before subscribing: %v", err)
		}
		var frame map[string]any
		if json.Unmarshal(data, &frame) != nil {
			continue
		}
		switch frame["type"] {
		case "ready":
			request, _ := json.Marshal(map[string]any{"action": "subscribe", "rooms": []string{room}})
			if err := socket.Write(ctx, websocket.MessageText, request); err != nil {
				t.Fatalf("subscribing: %v", err)
			}
		case "subscribed":
			subscribed = true
		}
	}

	makeDue(t, pool)
	if sent := serverOf(handler).DeliverScheduled(context.Background()); sent != 1 {
		t.Fatalf("the delivery pass sent %d messages, want 1", sent)
	}
	serverOf(handler).WaitForBackground()

	for {
		_, data, err := socket.Read(ctx)
		if err != nil {
			t.Fatalf("the scheduled message never arrived live: %v", err)
		}
		if strings.Contains(string(data), `"type":"event"`) && strings.Contains(string(data), "the meetup is at six") {
			break
		}
	}

	mentioned := false
	for _, hit := range notificationsFor(t, handler, reader) {
		if hit.Reason == "mention" {
			mentioned = true
		}
	}
	if !mentioned {
		t.Error("the person a scheduled message mentions was not told")
	}
}

/*
What would have stopped the message being typed stops it being sent.

Scheduled before the ban, due after it: had the pass not asked again, a ban
would have been a way to post that a moderator could not see coming.
*/
func TestABanStopsAMessageScheduledBeforeIt(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	pool := scheduledDatabase(t)

	room := makeRoom(t, handler, admin, "general", "public")
	writer := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, writer, http.StatusOK)

	id := schedule(t, handler, writer, room, "posted from beyond a ban")
	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "before the ban"}, writer, http.StatusCreated))
	eventID, _ := sent["id"].(string)

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/bans",
		map[string]any{"user": accountID(t, handler, writer), "reason": "spam"}, admin, http.StatusNoContent)

	// Every way of putting words in the channel is closed, not only the
	// obvious one: only sending used to ask.
	for _, attempt := range []struct{ method, path string }{
		{http.MethodPost, "/messages"},
		{http.MethodPost, "/threads/" + eventID + "/replies"},
		{http.MethodPatch, "/messages/" + eventID},
		{http.MethodPost, "/scheduled"},
	} {
		recorder := call(t, handler, attempt.method, "/api/rooms/"+room+attempt.path,
			map[string]any{"body": "still here", "sendAt": whenSoon()}, writer)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s while banned: status %d, want 403", attempt.method, attempt.path, recorder.Code)
		}
	}

	makeDue(t, pool)
	if sent := serverOf(handler).DeliverScheduled(context.Background()); sent != 0 {
		t.Fatalf("a banned sender's scheduled message went out (%d sent)", sent)
	}

	var reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT failed_reason FROM scheduled_messages WHERE id = $1`, id).Scan(&reason); err != nil {
		t.Fatalf("reading what became of it: %v", err)
	}
	if reason == "" {
		t.Error("the message was neither sent nor recorded as failed")
	}
}

// And a withdrawn channel.post stops it the same way.
func TestAWithdrawnPermissionStopsAScheduledMessage(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	pool := scheduledDatabase(t)

	room := makeRoom(t, handler, admin, "announcements", "public")
	writer := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, writer, http.StatusOK)

	id := schedule(t, handler, writer, room, "before it became read-only")
	withdraw(t, handler, admin, "channel.post", room)

	makeDue(t, pool)
	if sent := serverOf(handler).DeliverScheduled(context.Background()); sent != 0 {
		t.Fatalf("a message went out where its sender may no longer post (%d sent)", sent)
	}

	// The sender is told, in words, beside the message that did not go.
	var reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT failed_reason FROM scheduled_messages WHERE id = $1`, id).Scan(&reason); err != nil {
		t.Fatalf("reading what became of it: %v", err)
	}
	if !strings.Contains(reason, "no longer post") {
		t.Errorf("the sender is told %q, which does not say why", reason)
	}
}

/*
Two servers making a pass at once send each message once.

Two passes in one process stand in for two nodes: what matters is two
concurrent queries against one database, and that is what this is.
*/
func TestConcurrentPassesSendEachMessageOnce(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	pool := scheduledDatabase(t)

	room := makeRoom(t, handler, admin, "general", "public")
	const count = 30
	for range count {
		schedule(t, handler, admin, room, "once only")
	}
	makeDue(t, pool)

	server := serverOf(handler)
	var wait sync.WaitGroup
	totals := make([]int, 4)
	for i := range totals {
		wait.Go(func() { totals[i] = server.DeliverScheduled(context.Background()) })
	}
	wait.Wait()
	server.WaitForBackground()

	sent := 0
	for _, total := range totals {
		sent += total
	}
	if sent != count {
		t.Errorf("the passes reported %d sends between them, want %d", sent, count)
	}

	var stored int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM events
		WHERE room_id = $1 AND content->>'body' = 'once only'`, room).Scan(&stored); err != nil {
		t.Fatalf("counting what was sent: %v", err)
	}
	if stored != count {
		t.Errorf("%d messages were stored for %d scheduled; concurrent passes sent some twice", stored, count)
	}
}
