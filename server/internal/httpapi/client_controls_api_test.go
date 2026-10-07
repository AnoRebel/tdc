package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

/*
What the clients' own controls read and write: somebody's status as they set
it, whether they announce their typing, which conversations they have muted,
and the messages they have scheduled.
*/

// Somebody sees their own status in full — the note, when it lapses, and
// that they appear offline — even though nobody else is shown any of it.
func TestOwnStatusIsReadBackInFull(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	fresh := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/status", nil, token, http.StatusOK))
	if fresh["status"] != "available" || fresh["invisible"] != false || fresh["expiresAt"] != nil {
		t.Errorf("a fresh account's own status = %v, want available, visible, no expiry", fresh)
	}

	set := decode(t, mustCall(t, handler, http.MethodPut, "/api/me/status", map[string]any{
		"status": "busy", "text": "in a meeting", "expiresInSeconds": 1800, "invisible": true,
	}, token, http.StatusOK))

	for _, own := range []map[string]any{
		set,
		decode(t, mustCall(t, handler, http.MethodGet, "/api/me/status", nil, token, http.StatusOK)),
	} {
		if own["status"] != "busy" || own["statusText"] != "in a meeting" || own["invisible"] != true {
			t.Errorf("own status = %v, want busy, its note, and invisible", own)
		}
		raw, _ := own["expiresAt"].(string)
		expires, err := time.Parse(time.RFC3339, raw)
		if err != nil || expires.Sub(time.Now().Add(30*time.Minute)).Abs() > time.Minute {
			t.Errorf("expiresAt = %q, want about thirty minutes from now", raw)
		}
	}

	// Setting it again without an expiry keeps it until changed.
	kept := decode(t, mustCall(t, handler, http.MethodPut, "/api/me/status", map[string]any{
		"status": "away", "invisible": false,
	}, token, http.StatusOK))
	if kept["status"] != "away" || kept["invisible"] != false || kept["expiresAt"] != nil {
		t.Errorf("own status = %v, want away, visible, no expiry", kept)
	}
}

// Turning typing off is honoured by the server, so no client can show it.
func TestTypingCanBeTurnedOff(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	room := makeRoom(t, handler, admin, "general", "public")

	writer := registerAndSignIn(t, handler, "amina")
	reader := registerAndSignIn(t, handler, "baraka")
	for _, person := range []string{writer, reader} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, person, http.StatusOK)
	}

	privacy := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/privacy", nil, writer, http.StatusOK))
	if privacy["sendsTypingIndication"] != true {
		t.Fatalf("sendsTypingIndication = %v by default, want true", privacy["sendsTypingIndication"])
	}

	mustCall(t, handler, http.MethodPut, "/api/me/privacy",
		map[string]any{"sendsTypingIndication": false}, writer, http.StatusNoContent)

	privacy = decode(t, mustCall(t, handler, http.MethodGet, "/api/me/privacy", nil, writer, http.StatusOK))
	if privacy["sendsTypingIndication"] != false {
		t.Errorf("sendsTypingIndication = %v after turning it off", privacy["sendsTypingIndication"])
	}
	// And nothing else moved: a request naming one setting leaves the others.
	if privacy["directMessages"] != "anyone" || privacy["showsPresence"] != true {
		t.Errorf("privacy = %v, want the other settings untouched", privacy)
	}

	typing := func() []any {
		return decodeList(t, mustCall(t, handler, http.MethodGet,
			"/api/rooms/"+room+"/typing", nil, reader, http.StatusOK))
	}

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/typing", nil, writer, http.StatusNoContent)
	if shown := typing(); len(shown) != 0 {
		t.Errorf("typing = %v from somebody who turned it off, want nobody", shown)
	}

	mustCall(t, handler, http.MethodPut, "/api/me/privacy",
		map[string]any{"sendsTypingIndication": true}, writer, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/typing", nil, writer, http.StatusNoContent)
	if shown := typing(); len(shown) != 1 {
		t.Errorf("typing = %v once turned back on, want the writer", shown)
	}
}

// What somebody has muted is read back, and is theirs alone.
func TestNotificationPreferencesAreReadBack(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	room := makeRoom(t, handler, admin, "general", "public")
	other := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, other, http.StatusOK)

	read := func(token string) map[string]any {
		return decode(t, mustCall(t, handler, http.MethodGet,
			"/api/me/preferences/notifications", nil, token, http.StatusOK))
	}

	fresh := read(admin)
	if def, _ := fresh["default"].(map[string]any); def["level"] != "mentions" || def["muted"] != false {
		t.Errorf("default = %v, want mentions and not muted", fresh["default"])
	}

	mustCall(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"room": room, "level": "all", "muted": true}, admin, http.StatusNoContent)
	mustCall(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "none"}, admin, http.StatusNoContent)

	after := read(admin)
	rooms, _ := after["rooms"].(map[string]any)
	if set, _ := rooms[room].(map[string]any); set["level"] != "all" || set["muted"] != true {
		t.Errorf("rooms = %v, want the channel at all and muted", after["rooms"])
	}
	if def, _ := after["default"].(map[string]any); def["level"] != "none" {
		t.Errorf("default = %v, want none", after["default"])
	}

	if theirs, _ := read(other)["rooms"].(map[string]any); len(theirs) != 0 {
		t.Errorf("somebody else's preferences = %v, want none of the first person's", theirs)
	}
}

// A scheduled message can be reworded, moved and sent early — by its author.
func TestAScheduledMessageCanBeChangedAndSentNow(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	room := makeRoom(t, handler, admin, "general", "public")
	stranger := registerAndSignIn(t, handler, "amina")

	id := schedule(t, handler, admin, room, "draft wording")
	later := time.Now().Add(2 * time.Hour).Format(time.RFC3339)

	mustCall(t, handler, http.MethodPatch, "/api/me/scheduled/"+id,
		map[string]any{"body": "taken over"}, stranger, http.StatusNotFound)
	mustCall(t, handler, http.MethodPost, "/api/me/scheduled/"+id+"/send", nil, stranger, http.StatusNotFound)
	mustCall(t, handler, http.MethodPatch, "/api/me/scheduled/"+id,
		map[string]any{"sendAt": time.Now().Add(-time.Minute).Format(time.RFC3339)}, admin, http.StatusBadRequest)

	changed := decode(t, mustCall(t, handler, http.MethodPatch, "/api/me/scheduled/"+id,
		map[string]any{"body": "final wording", "sendAt": later}, admin, http.StatusOK))
	if changed["body"] != "final wording" {
		t.Errorf("body = %v, want the new wording", changed["body"])
	}
	at, _ := changed["sendAt"].(float64)
	if want, _ := time.Parse(time.RFC3339, later); int64(at) != want.UnixMilli() {
		t.Errorf("sendAt = %v, want %v", at, want.UnixMilli())
	}

	// Moving it alone keeps the wording.
	moved := decode(t, mustCall(t, handler, http.MethodPatch, "/api/me/scheduled/"+id,
		map[string]any{"sendAt": whenSoon()}, admin, http.StatusOK))
	if moved["body"] != "final wording" {
		t.Errorf("body = %v after moving it, want it kept", moved["body"])
	}

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/me/scheduled/"+id+"/send", nil, admin, http.StatusCreated))
	serverOf(handler).WaitForBackground()
	if !strings.Contains(sent["content"].(map[string]any)["body"].(string), "final wording") {
		t.Errorf("sent = %v, want the final wording", sent)
	}
	timeline := mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/timeline", nil, admin, http.StatusOK)
	if !strings.Contains(timeline.Body.String(), "final wording") {
		t.Error("the message sent early is not in the channel")
	}

	if left := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/me/scheduled", nil, admin, http.StatusOK)); len(left) != 0 {
		t.Errorf("scheduled = %v after sending it, want nothing outstanding", left)
	}
	// And it cannot go twice.
	mustCall(t, handler, http.MethodPost, "/api/me/scheduled/"+id+"/send", nil, admin, http.StatusNotFound)
}

// A message a delivery pass holds is being sent, and cannot be cancelled,
// changed or sent a second time from under it.
func TestAScheduledMessageBeingSentIsLeftAlone(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	pool := scheduledDatabase(t)
	room := makeRoom(t, handler, admin, "general", "public")

	id := schedule(t, handler, admin, room, "on its way")
	if _, err := pool.Exec(context.Background(),
		`UPDATE scheduled_messages SET claimed_until = now() + interval '5 minutes' WHERE id = $1`, id); err != nil {
		t.Fatalf("claiming: %v", err)
	}

	mustCall(t, handler, http.MethodDelete, "/api/me/scheduled/"+id, nil, admin, http.StatusNotFound)
	mustCall(t, handler, http.MethodPatch, "/api/me/scheduled/"+id,
		map[string]any{"body": "changed"}, admin, http.StatusNotFound)
	mustCall(t, handler, http.MethodPost, "/api/me/scheduled/"+id+"/send", nil, admin, http.StatusNotFound)
}
