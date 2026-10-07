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
Merging a held call into another invites its people into the other call only:
they join it and are heard there, and the conversation that call belongs to
stays closed to them. Somebody not in a call with the person merging is not
invited, and nobody uninvited can reach the call by its identifier.
*/
func TestMergingACallLetsItsPeopleIntoTheCallOnly(t *testing.T) {
	handler, admin, _, amina, _ := withCalls(t)
	// A private channel, so being let into its call is all that admits anybody.
	room := makeRoom(t, handler, admin, "board", "private")
	issued := issueInvitation(t, handler, admin, room, map[string]any{})
	if code := redeem(t, handler, amina, map[string]any{"token": issued.Token}); code != http.StatusOK {
		t.Fatalf("joining the private channel: status %d", code)
	}
	chausiku := registerAndSignIn(t, handler, "chausiku")
	outsider := registerAndSignIn(t, handler, "outsider")
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	device := liveConnection(t, ctx, server, chausiku)

	// amina talks with the admin in the channel, and holds a direct call with chausiku.
	channelCall := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, admin, http.StatusOK))
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, amina, http.StatusOK)
	direct := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct", map[string]any{"usernames": []string{"chausiku"}}, amina, http.StatusOK))
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+direct["id"].(string)+"/call", map[string]any{"audio": true}, amina, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+direct["id"].(string)+"/call", map[string]any{"audio": true}, chausiku, http.StatusOK)

	merged := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/invite",
		map[string]any{"users": []string{accountID(t, handler, chausiku), accountID(t, handler, outsider)}}, amina, http.StatusOK))
	if got, _ := merged["invited"].([]any); len(got) != 1 || merged["notInvited"] != float64(1) {
		t.Fatalf("merging invited %v", merged)
	}

	frame, ok := nextCallFrame(t, ctx, device, "call.ring")
	if !ok {
		t.Fatal("the person merged in was never rung")
	}
	if text := mustJSON(t, frame); !strings.Contains(text, `"conference":true`) || strings.Contains(text, "board") {
		t.Errorf("the invitation should name nothing of the conversation: %s", text)
	}

	callID := channelCall["id"].(string)
	if code := call(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, chausiku).Code; code == http.StatusOK {
		t.Error("somebody merged in reached the call through its conversation")
	}
	joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, chausiku, http.StatusOK))
	if got := participantsOf(joined); len(got) != 3 {
		t.Errorf("after the merge the call holds %v", got)
	}
	for _, path := range []string{"/timeline", "/members", ""} {
		if code := call(t, handler, http.MethodGet, "/api/rooms/"+room+path, nil, chausiku).Code; code == http.StatusOK {
			t.Errorf("somebody merged into a call can read its conversation's %q", path)
		}
	}
	mustCall(t, handler, http.MethodPatch, "/api/calls/"+callID, map[string]any{"audio": false}, chausiku, http.StatusOK)

	mustCall(t, handler, http.MethodGet, "/api/calls/"+callID, nil, outsider, http.StatusNotFound)
	mustCall(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, outsider, http.StatusNotFound)

	mustCall(t, handler, http.MethodDelete, "/api/calls/"+callID, nil, chausiku, http.StatusNoContent)
}
