package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// callFrom calls the API as if from a given address, for limits kept per address.
func callFrom(t *testing.T, handler http.Handler, address, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = address + ":4000"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// A private channel with a call in progress, held by the administrator.
func privateCall(t *testing.T) (http.Handler, string, string, string) {
	t.Helper()
	handler, admin, _, _, _ := withCalls(t)
	room := makeRoom(t, handler, admin, "board", "private")
	call := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, admin, http.StatusOK))
	return handler, admin, room, call["id"].(string)
}

func makeLink(t *testing.T, handler http.Handler, admin, room string, options map[string]any) string {
	t.Helper()
	link := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/links", options, admin, http.StatusCreated))
	secret, _ := link["secret"].(string)
	if !strings.HasPrefix(secret, "tdcl_") {
		t.Fatalf("making a link answered %v", link)
	}
	return secret
}

/*
A link lets a signed-in person from elsewhere on the server into the call and
nothing else, says only the conversation's name and who is in the call, and
needs call.link to make — which nobody holds by default.
*/
func TestACallLinkAdmitsToTheCallOnly(t *testing.T) {
	handler, admin, room, callID := privateCall(t)
	stranger := registerAndSignIn(t, handler, "chausiku")

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/links", map[string]any{}, stranger, http.StatusForbidden)
	secret := makeLink(t, handler, admin, room, map[string]any{"label": "Standup guests"})

	target := decode(t, mustCall(t, handler, http.MethodGet, "/api/call-links/"+secret, nil, "", http.StatusOK))
	if target["roomName"] != "board" || target["started"] != true || target["protected"] != false {
		t.Errorf("the link describes %v", target)
	}
	if text := mustJSON(t, target); strings.Contains(text, room) || strings.Contains(text, "members") {
		t.Errorf("reading a link gives away more than the name and who is in the call: %s", text)
	}

	use := decode(t, mustCall(t, handler, http.MethodPost, "/api/call-links/"+secret+"/join", map[string]any{}, stranger, http.StatusOK))
	if use["callId"] != callID || use["waiting"] != false {
		t.Fatalf("using the link answered %v", use)
	}
	joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, stranger, http.StatusOK))
	if got := participantsOf(joined); len(got) != 2 {
		t.Errorf("after joining by link the call holds %v", got)
	}
	for _, path := range []string{"/timeline", "/members", ""} {
		if code := call(t, handler, http.MethodGet, "/api/rooms/"+room+path, nil, stranger).Code; code == http.StatusOK {
			t.Errorf("somebody let in by a link can read the conversation's %q", path)
		}
	}
}

// An expired, used-up or revoked link refuses, and the refusals read alike.
func TestAnUnusableLinkRefuses(t *testing.T) {
	handler, admin, room, _ := privateCall(t)
	first := registerAndSignIn(t, handler, "chausiku")
	second := registerAndSignIn(t, handler, "outsider")

	once := makeLink(t, handler, admin, room, map[string]any{"maxUses": 1})
	mustCall(t, handler, http.MethodPost, "/api/call-links/"+once+"/join", map[string]any{}, first, http.StatusOK)
	usedUp := call(t, handler, http.MethodPost, "/api/call-links/"+once+"/join", map[string]any{}, second)

	revocable := makeLink(t, handler, admin, room, map[string]any{})
	links := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call/links", nil, admin, http.StatusOK))
	var id string
	for _, raw := range links {
		if link := raw.(map[string]any); link["uses"] == float64(0) {
			id = link["id"].(string)
		}
	}
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/call/links/"+id, nil, admin, http.StatusNoContent)
	revoked := call(t, handler, http.MethodPost, "/api/call-links/"+revocable+"/join", map[string]any{}, second)
	invented := call(t, handler, http.MethodGet, "/api/call-links/tdcl_invented", nil, "")

	for name, recorder := range map[string]*httptest.ResponseRecorder{"used up": usedUp, "revoked": revoked, "invented": invented} {
		if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "expired, been used up or been withdrawn") {
			t.Errorf("a %s link answered %d %s", name, recorder.Code, recorder.Body)
		}
	}
}

/*
A protected link counts wrong passcodes on itself, and past its limit refuses
the right one too — so spreading guesses across addresses does not get round
it — and every wrong one is in the audit log.
*/
func TestAProtectedLinkLocksAfterTooManyWrongPasscodes(t *testing.T) {
	handler, admin, room, _ := privateCall(t)
	stranger := registerAndSignIn(t, handler, "chausiku")
	secret := makeLink(t, handler, admin, room, map[string]any{"passcode": "kilimanjaro"})

	if code := call(t, handler, http.MethodPost, "/api/call-links/"+secret+"/join", map[string]any{"passcode": "wrong"}, stranger).Code; code != http.StatusForbidden {
		t.Fatalf("a wrong passcode answered %d", code)
	}
	for attempt := range 20 {
		address := fmt.Sprintf("198.51.100.%d", attempt+1)
		callFrom(t, handler, address, http.MethodPost, "/api/call-links/"+secret+"/join", map[string]any{"passcode": "guess"}, stranger)
	}
	right := callFrom(t, handler, "203.0.113.9", http.MethodPost, "/api/call-links/"+secret+"/join", map[string]any{"passcode": "kilimanjaro"}, stranger)
	if right.Code != http.StatusTooManyRequests {
		t.Errorf("the right passcode past the limit answered %d %s", right.Code, right.Body)
	}

	entries := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/admin/audit?action=call.link.passcode_failed", nil, admin, http.StatusOK))
	if len(entries) < 20 {
		t.Errorf("the audit log holds %d wrong passcodes, want at least 20", len(entries))
	}
}

// With a lobby, people wait until somebody in the call lets them in, and are
// told either way.
func TestALobbyHoldsPeopleUntilLetIn(t *testing.T) {
	handler, admin, room, callID := privateCall(t)
	waiting := registerAndSignIn(t, handler, "chausiku")
	turnedAway := registerAndSignIn(t, handler, "outsider")
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := liveConnection(t, ctx, server, admin)
	device := liveConnection(t, ctx, server, waiting)

	secret := makeLink(t, handler, admin, room, map[string]any{"lobby": true})
	use := decode(t, mustCall(t, handler, http.MethodPost, "/api/call-links/"+secret+"/join", map[string]any{}, waiting, http.StatusOK))
	if use["waiting"] != true {
		t.Fatalf("a lobby link let somebody straight in: %v", use)
	}
	if code := call(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, waiting).Code; code == http.StatusOK {
		t.Fatal("somebody waiting in the lobby joined without being let in")
	}
	if frame, ok := nextCallFrame(t, ctx, host, "call.lobby"); !ok || !strings.Contains(mustJSON(t, frame), "chausiku") {
		t.Fatalf("the call was not told who is waiting: %v", frame)
	}

	waitingID := accountID(t, handler, waiting)
	mustCall(t, handler, http.MethodPost, "/api/calls/"+callID+"/lobby/"+waitingID+"/admit", nil, admin, http.StatusNoContent)
	if _, ok := nextCallFrame(t, ctx, device, "call.admitted"); !ok {
		t.Error("the person let in was not told")
	}
	mustCall(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, waiting, http.StatusOK)

	mustCall(t, handler, http.MethodPost, "/api/call-links/"+secret+"/join", map[string]any{}, turnedAway, http.StatusOK)
	// Somebody let in by a link does not let others in after them.
	mustCall(t, handler, http.MethodPost, "/api/calls/"+callID+"/lobby/"+accountID(t, handler, turnedAway)+"/admit", nil, waiting, http.StatusNotFound)
	mustCall(t, handler, http.MethodPost, "/api/calls/"+callID+"/lobby/"+accountID(t, handler, turnedAway)+"/turn-away", nil, admin, http.StatusNoContent)
	if code := call(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, turnedAway).Code; code == http.StatusOK {
		t.Error("somebody turned away joined anyway")
	}
}
