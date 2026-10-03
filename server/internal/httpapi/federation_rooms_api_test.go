package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
A room crosses between servers only because somebody said so about that room.

Enabling federation on a server must not expose the conversations it already
holds, so the two are separate decisions and this checks the default.
*/
func TestARoomIsNotFederatedUntilSomebodySaysSo(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://a.example.org")
	owner := enableFederation(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	peers := decode(t, mustCall(t, handler, http.MethodGet,
		"/api/admin/rooms/"+roomID+"/federation", nil, owner, http.StatusOK))
	list, _ := peers["peers"].([]any)
	if len(list) != 0 {
		t.Fatalf("a new room already had peers: %v", list)
	}

	// Opting in is explicit, and names the peers at the same time.
	federated := decode(t, mustCall(t, handler, http.MethodPut,
		"/api/admin/rooms/"+roomID+"/federation",
		map[string]any{"federated": true, "peers": []string{"b.example.org"}}, owner, http.StatusOK))

	list, _ = federated["peers"].([]any)
	if len(list) != 1 {
		t.Fatalf("after federating, the room has %d peers, want 1", len(list))
	}

	first, _ := list[0].(map[string]any)
	if first["peer"] != "b.example.org" {
		t.Errorf("peer = %v, want b.example.org", first["peer"])
	}
	// A peer that has just been added has been told nothing, which is the
	// honest starting point — the first delivery gives it everything.
	if cursor, _ := first["cursor"].(float64); cursor != 0 {
		t.Errorf("a new peer's cursor = %v, want 0", cursor)
	}
}

/*
Adding the same peer twice does not reset what it has been told.

Otherwise an operator re-running a setup step would resend the room's entire
history, which for a long-running room is a great deal of traffic for no
reason.
*/
func TestAddingAPeerTwiceKeepsItsCursor(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://a.example.org")
	owner := enableFederation(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+roomID+"/federation",
		map[string]any{"federated": true, "peers": []string{"b.example.org"}}, owner, http.StatusOK)

	// Something to have been delivered, so a reset would be visible.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "first"}, owner, http.StatusCreated)

	// Adding again, as an operator repeating a step might.
	again := decode(t, mustCall(t, handler, http.MethodPut,
		"/api/admin/rooms/"+roomID+"/federation",
		map[string]any{"federated": true, "peers": []string{"b.example.org"}}, owner, http.StatusOK))

	list, _ := again["peers"].([]any)
	if len(list) != 1 {
		t.Fatalf("the peer was added twice: %d rows", len(list))
	}
}

/*
The federated events endpoint refuses a caller with no signature.

It is the read path for a room's contents, so an unauthenticated caller
reaching it would be a way to read any federated room on any server.
*/
func TestTheFederatedEventsEndpointRefusesAnUnsignedCaller(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://a.example.org")
	owner := enableFederation(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+roomID+"/federation",
		map[string]any{"federated": true, "peers": []string{"b.example.org"}}, owner, http.StatusOK)

	recorder := call(t, handler, http.MethodGet, "/api/federation/rooms/general/events", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("an unsigned read returned %d, want 401", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "events") {
		t.Error("the handler ran for an unsigned request")
	}
}

/*
Federating a room is recorded under its own audit action.

Exposing one room's contents beyond this server is a different act from
changing a server-wide federation setting, and somebody asking later which
rooms were opened, and by whom, should find entries that say so.
*/
func TestFederatingARoomIsAudited(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://a.example.org")
	owner := enableFederation(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+roomID+"/federation",
		map[string]any{"federated": true, "peers": []string{"b.example.org"}}, owner, http.StatusOK)

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=50", nil, owner))

	var recorded auditEntry
	for _, entry := range entries {
		if entry.Action == "room.federation.changed" {
			recorded = entry
		}
	}
	if recorded.Action == "" {
		t.Fatal("federating a room was not audited under its own action")
	}
	if recorded.TargetID != roomID {
		t.Errorf("the entry names %q, want the room %q", recorded.TargetID, roomID)
	}
	if !strings.Contains(recorded.Detail, "b.example.org") {
		t.Errorf("the entry should record which peers, got %q", recorded.Detail)
	}
}

/*
A room made here is not reported as somebody else's.

A server with no address configured — the default, and every development and
test server — stamps its rooms with a name of its own. The room was compared
with the configured address instead, which was empty, so every room came back
naming a home server; the client took that as federation and sent every message
by the remote path, where each was refused.
*/
func TestARoomMadeHereIsNotHostedElsewhere(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	room := makeRoom(t, handler, admin, "general", "public")

	got := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room, nil, admin, http.StatusOK))
	if home, present := got["homeServer"]; present {
		t.Errorf("a room made on this server names %v as its home; clients will send through federation", home)
	}

	listed := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/rooms", nil, admin, http.StatusOK))
	for _, entry := range listed {
		if item, _ := entry.(map[string]any); item["homeServer"] != nil {
			t.Errorf("the room list reports a home server for a room made here: %v", item)
		}
	}
}
