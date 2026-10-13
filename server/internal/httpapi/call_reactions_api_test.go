package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

/*
Hands go up in order and come down by their owner, by whoever presents or by a
moderator — nobody else. Reactions reach the people in the call and nobody
outside it, and only from the fixed set.
*/
func TestHandsAndReactionsInACall(t *testing.T) {
	handler, admin, room, amina, baraka := withCalls(t)
	outsider := registerAndSignIn(t, handler, "chausiku")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, outsider, http.StatusOK)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inCall := liveConnection(t, ctx, server, baraka)
	notInCall := liveConnection(t, ctx, server, outsider)

	for _, person := range []string{admin, amina, baraka} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, person, http.StatusOK)
	}
	call := "/api/rooms/" + room + "/call"

	mustCall(t, handler, http.MethodPut, call+"/hand", nil, baraka, http.StatusOK)
	time.Sleep(10 * time.Millisecond)
	state := decode(t, mustCall(t, handler, http.MethodPut, call+"/hand", nil, amina, http.StatusOK))
	var barakaAt, aminaAt string
	for _, raw := range state["participants"].([]any) {
		p := raw.(map[string]any)
		at, _ := p["handRaisedAt"].(string)
		switch p["username"] {
		case "baraka":
			barakaAt = at
		case "amina":
			aminaAt = at
		}
	}
	if barakaAt == "" || aminaAt == "" || !(barakaAt < aminaAt) {
		t.Errorf("hands should be up in the order raised: baraka %q, amina %q", barakaAt, aminaAt)
	}

	// Not anybody's to lower but its owner's, the presenter's or a moderator's.
	mustCall(t, handler, http.MethodDelete, call+"/hand/"+accountID(t, handler, amina), nil, baraka, http.StatusForbidden)
	mustCall(t, handler, http.MethodDelete, call+"/hand/"+accountID(t, handler, amina), nil, admin, http.StatusOK)
	mustCall(t, handler, http.MethodDelete, call+"/hand", nil, baraka, http.StatusOK)

	mustCall(t, handler, http.MethodPost, call+"/reactions", map[string]any{"kind": "clap"}, amina, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, call+"/reactions", map[string]any{"kind": "<script>"}, amina, http.StatusBadRequest)
	mustCall(t, handler, http.MethodPost, call+"/reactions", map[string]any{"kind": "clap"}, outsider, http.StatusNotFound)

	if frame, ok := nextCallFrame(t, ctx, inCall, "call.reaction"); !ok || !strings.Contains(mustJSON(t, frame), `"kind":"clap"`) {
		t.Errorf("somebody in the call did not get the reaction: %v", frame)
	}
	short, stop := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer stop()
	if frame, ok := nextCallFrame(t, short, notInCall, "call.reaction"); ok {
		t.Errorf("somebody outside the call got a reaction: %v", frame)
	}
}
