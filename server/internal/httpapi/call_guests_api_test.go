package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
A guest gets into a call by a link that allows guests, on a server that
allows them; reaches that call and nothing else; is marked as a guest to
everybody in it; is nobody to find among the server's people; and is shut out
once the call ends.
*/
func TestAGuestReachesTheCallAndNothingElse(t *testing.T) {
	handler, admin, room, _ := privateCall(t)
	guestsLink := makeLink(t, handler, admin, room, map[string]any{"guests": true})
	membersLink := makeLink(t, handler, admin, room, map[string]any{})

	// Off on the server: refused, whatever the link says.
	mustCall(t, handler, http.MethodPost, "/api/call-links/"+guestsLink+"/guest", map[string]any{"name": "Neema"}, "", http.StatusForbidden)
	setSetting(t, handler, admin, "calls.guests", "true")
	// A link not made for guests refuses them.
	mustCall(t, handler, http.MethodPost, "/api/call-links/"+membersLink+"/guest", map[string]any{"name": "Neema"}, "", http.StatusForbidden)
	mustCall(t, handler, http.MethodPost, "/api/call-links/"+guestsLink+"/guest", map[string]any{"name": "  "}, "", http.StatusBadRequest)

	joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/call-links/"+guestsLink+"/guest", map[string]any{"name": "Neema"}, "", http.StatusOK))
	token, _ := joined["token"].(string)
	use := joined["use"].(map[string]any)
	callID := use["callId"].(string)
	if user := joined["user"].(map[string]any); user["guest"] != true || user["displayName"] != "Neema" {
		t.Fatalf("the guest is %v", user)
	}

	state := decode(t, mustCall(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, token, http.StatusOK))
	if !strings.Contains(mustJSON(t, state), `"guest":true`) {
		t.Errorf("the call does not mark the guest: %v", state)
	}
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/reactions", map[string]any{"kind": "clap"}, token, http.StatusNoContent)
	mustCall(t, handler, http.MethodGet, "/api/me", nil, token, http.StatusOK)

	for _, path := range []string{"/api/rooms/" + room + "/timeline", "/api/rooms", "/api/directory/people", "/api/people/search?q=a", "/api/admin/users"} {
		if code := call(t, handler, http.MethodGet, path, nil, token).Code; code != http.StatusForbidden {
			t.Errorf("a guest reached %s: %d", path, code)
		}
	}
	people := mustJSON(t, decodeList(t, mustCall(t, handler, http.MethodGet, "/api/admin/users", nil, admin, http.StatusOK)))
	if strings.Contains(people, "Neema") {
		t.Error("the guest is listed among the server's people")
	}

	// The last member leaves: the call ends though the guest is still in it,
	// and the guest's session goes with it.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/call", nil, admin, http.StatusNoContent)
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, admin, http.StatusNotFound)
	mustCall(t, handler, http.MethodGet, "/api/me", nil, token, http.StatusUnauthorized)
}
