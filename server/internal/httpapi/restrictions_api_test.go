package httpapi_test

import (
	"context"
	"net/http"
	"testing"
)

/*
A posting restriction silences somebody in one channel and leaves them in it.

The moderation service could always record one and the send route always
honoured it, but nothing let a moderator make one, and only the send route
asked: a reply in a thread, an edit, a forward or a scheduled message all went
through. This makes one the way a moderator would, then tries every way in.
*/
func TestARestrictionSilencesEveryWayOfPosting(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	pool := scheduledDatabase(t)

	room := makeRoom(t, handler, admin, "general", "public")
	other := makeRoom(t, handler, admin, "random", "public")
	member := registerAndSignIn(t, handler, "amina")
	for _, r := range []string{room, other} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+r+"/join", nil, member, http.StatusOK)
	}

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "before"}, member, http.StatusCreated))
	eventID, _ := sent["id"].(string)
	elsewhere := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+other+"/messages",
		map[string]any{"body": "worth sharing"}, member, http.StatusCreated))
	elsewhereID, _ := elsewhere["id"].(string)
	schedule(t, handler, member, room, "scheduled before the restriction")

	// A reason is required, as it is for a ban.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/restrictions",
		map[string]any{"user": accountID(t, handler, member)}, admin, http.StatusBadRequest)
	restricted := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/restrictions",
		map[string]any{"user": accountID(t, handler, member), "reason": "cool off", "hours": 2},
		admin, http.StatusCreated))
	restrictionID, _ := restricted["id"].(string)

	listed := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/restrictions",
		nil, admin, http.StatusOK))
	first := map[string]any{}
	if len(listed) == 1 {
		first, _ = listed[0].(map[string]any)
	}
	if first["username"] != "amina" || first["expiresAt"] == nil {
		t.Errorf("the channel's restrictions are %v, want amina's, with when it ends", listed)
	}

	for _, attempt := range []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPost, "/api/rooms/" + room + "/messages", map[string]any{"body": "hello"}},
		{http.MethodPost, "/api/rooms/" + room + "/threads/" + eventID + "/replies", map[string]any{"body": "hello"}},
		{http.MethodPatch, "/api/rooms/" + room + "/messages/" + eventID, map[string]any{"body": "a paragraph instead"}},
		{http.MethodPost, "/api/rooms/" + room + "/scheduled", map[string]any{"body": "later", "sendAt": whenSoon()}},
		{http.MethodPost, "/api/rooms/" + other + "/messages/" + elsewhereID + "/forward", map[string]any{"to": room}},
	} {
		if code := call(t, handler, attempt.method, attempt.path, attempt.body, member).Code; code != http.StatusForbidden {
			t.Errorf("%s %s while restricted: status %d, want 403", attempt.method, attempt.path, code)
		}
	}

	// Still in the channel, still reading it, and free everywhere else.
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/timeline", nil, member, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+other+"/messages",
		map[string]any{"body": "fine here"}, member, http.StatusCreated)

	// And what was scheduled before is held to it when it comes due.
	makeDue(t, pool)
	if sent := serverOf(handler).DeliverScheduled(context.Background()); sent != 0 {
		t.Errorf("a message scheduled before the restriction went out during it (%d sent)", sent)
	}

	// Lifted only from the channel it was made in.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+other+"/restrictions/"+restrictionID,
		nil, admin, http.StatusNotFound)
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/restrictions/"+restrictionID,
		nil, admin, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "back"}, member, http.StatusCreated)
}

// Restricting takes moderation.restrict, as banning does.
func TestRestrictingTakesTheModerationPermission(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	room := makeRoom(t, handler, admin, "general", "public")
	member := registerAndSignIn(t, handler, "amina")
	other := registerAndSignIn(t, handler, "baraka")

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/restrictions",
		map[string]any{"user": accountID(t, handler, other), "reason": "because"}, member, http.StatusForbidden)
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/restrictions", nil, member, http.StatusForbidden)
}
