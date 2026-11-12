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

/*
A restriction across the server silences somebody in every channel at once,
and they go on reading all of them.

Each scope is lifted only from its own route: a channel's restriction is not
reachable through the server-wide one, nor the server's through a channel's,
so a moderator of one channel cannot undo a decision made for the whole server.
*/
func TestARestrictionAcrossTheServerSilencesEveryChannel(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	random := makeRoom(t, handler, admin, "random", "public")
	member := registerAndSignIn(t, handler, "amina")
	for _, room := range []string{general, random} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, member, http.StatusOK)
	}
	person := accountID(t, handler, member)

	mustCall(t, handler, http.MethodPost, "/api/admin/restrictions",
		map[string]any{"user": person}, admin, http.StatusBadRequest)
	everywhere := decode(t, mustCall(t, handler, http.MethodPost, "/api/admin/restrictions",
		map[string]any{"user": person, "reason": "a week off"}, admin, http.StatusCreated))
	everywhereID, _ := everywhere["id"].(string)

	for _, room := range []string{general, random} {
		if code := call(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
			map[string]any{"body": "hello"}, member).Code; code != http.StatusForbidden {
			t.Errorf("posting in %s while restricted everywhere: status %d, want 403", room, code)
		}
		mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/timeline", nil, member, http.StatusOK)
	}

	listed := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/admin/restrictions",
		nil, admin, http.StatusOK))
	if len(listed) != 1 {
		t.Errorf("the server-wide restrictions are %v, want amina's alone", listed)
	}
	if inChannel := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+general+"/restrictions",
		nil, admin, http.StatusOK)); len(inChannel) != 0 {
		t.Errorf("a server-wide restriction is listed as one channel's: %v", inChannel)
	}

	// A channel's own restriction, alongside.
	inGeneral := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/restrictions",
		map[string]any{"user": person, "reason": "and here especially"}, admin, http.StatusCreated))
	inGeneralID, _ := inGeneral["id"].(string)

	// Neither scope reaches the other's.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+general+"/restrictions/"+everywhereID,
		nil, admin, http.StatusNotFound)
	mustCall(t, handler, http.MethodDelete, "/api/admin/restrictions/"+inGeneralID,
		nil, admin, http.StatusNotFound)

	// Lifted where it was made, the other channels open again; the channel's
	// own restriction still holds where it was made.
	mustCall(t, handler, http.MethodDelete, "/api/admin/restrictions/"+everywhereID,
		nil, admin, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+random+"/messages",
		map[string]any{"body": "back"}, member, http.StatusCreated)
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "here too?"}, member).Code; code != http.StatusForbidden {
		t.Errorf("lifting the server-wide restriction lifted the channel's too: status %d", code)
	}
}
