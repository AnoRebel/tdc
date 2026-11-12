package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// sendMessage posts a message and returns its identifier.
func sendMessage(t *testing.T, handler http.Handler, token, roomID, body string) string {
	t.Helper()

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": body}, token, http.StatusCreated))
	id, _ := sent["id"].(string)
	return id
}

func TestThreadRepliesThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	rootID := sendMessage(t, handler, token, roomID, "what does everyone think?")

	for _, reply := range []string{"sounds good", "agreed"} {
		mustCall(t, handler, http.MethodPost,
			"/api/rooms/"+roomID+"/threads/"+rootID+"/replies",
			map[string]string{"body": reply}, token, http.StatusCreated)
	}

	thread := decode(t, mustCall(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/threads/"+rootID, nil, token, http.StatusOK))

	replies, _ := thread["replies"].([]any)
	if len(replies) != 2 {
		t.Fatalf("thread has %d replies, want 2", len(replies))
	}

	// The replies must not also appear in the channel timeline.
	var timeline []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	for _, event := range timeline {
		if body, _ := event["content"].(map[string]any)["body"].(string); body == "sounds good" {
			t.Error("a thread reply appeared in the channel timeline")
		}
	}
}

// A direct conversation is opened by naming its participants, and reopening it
// returns the same one.
func TestDirectConversationThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)
	ownerToken := completeSetup(t, handler)
	otherToken := registerAndSignIn(t, handler, "amina")

	other := decode(t, mustCall(t, handler, http.MethodGet, "/api/me", nil, otherToken, http.StatusOK))
	otherID, _ := other["id"].(string)

	opened := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{otherID}}, ownerToken, http.StatusOK))
	roomID, _ := opened["id"].(string)

	if opened["visibility"] != "private" || opened["kind"] != "direct" {
		t.Errorf("kind = %v, visibility = %v, want a private direct conversation",
			opened["kind"], opened["visibility"])
	}

	// Reopening returns the same conversation rather than making a second.
	reopened := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{otherID}}, ownerToken, http.StatusOK))
	if reopened["id"] != roomID {
		t.Error("reopening a direct conversation created a second one")
	}

	// Both participants see it in their list.
	for name, token := range map[string]string{"owner": ownerToken, "other": otherToken} {
		var listed []map[string]any
		recorder := call(t, handler, http.MethodGet, "/api/direct", nil, token)
		if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
			t.Fatalf("decode direct list for %s: %v", name, err)
		}
		found := false
		for _, room := range listed {
			if room["id"] == roomID {
				found = true
			}
		}
		if !found {
			t.Errorf("the conversation is missing from %s's list", name)
		}
	}

	// Both can post; a third person cannot even see it.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "just between us"}, ownerToken, http.StatusCreated)

	outsiderToken := registerAndSignIn(t, handler, "outsider")
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, outsiderToken)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("an outsider reading a direct conversation: status %d, want 404", recorder.Code)
	}
}

// Unread counts exclude a person's own messages and clear when they read.
func TestUnreadCountsThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)
	ownerToken := completeSetup(t, handler)
	readerToken := registerAndSignIn(t, handler, "amina")

	roomID := makeRoom(t, handler, ownerToken, "general", "public")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, readerToken, http.StatusOK)

	for range 3 {
		sendMessage(t, handler, ownerToken, roomID, "something to read")
	}

	unread := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/unread", nil, readerToken, http.StatusOK))
	if count, _ := unread[roomID].(float64); count != 3 {
		t.Errorf("unread = %v, want 3", unread[roomID])
	}

	// Reading to the latest position clears it.
	var timeline []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, readerToken)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	latest := timeline[len(timeline)-1]["sequence"]

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/read",
		map[string]any{"sequence": latest}, readerToken, http.StatusNoContent)

	cleared := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/unread", nil, readerToken, http.StatusOK))
	if count, _ := cleared[roomID].(float64); count != 0 {
		t.Errorf("unread = %v after reading, want 0", cleared[roomID])
	}
}

func TestStatusThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	// Only a status the operator configured may be chosen.
	var statuses []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/statuses", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &statuses); err != nil {
		t.Fatalf("decode statuses: %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("the server offers no statuses")
	}

	set := decode(t, mustCall(t, handler, http.MethodPut, "/api/me/status",
		map[string]any{"status": "busy", "text": "in a meeting"}, token, http.StatusOK))
	if set["status"] != "busy" || set["statusText"] != "in a meeting" {
		t.Errorf("status = %v, text = %v", set["status"], set["statusText"])
	}

	// An invented status is refused.
	refused := call(t, handler, http.MethodPut, "/api/me/status",
		map[string]any{"status": "inventing-my-own"}, token)
	if refused.Code != http.StatusBadRequest {
		t.Errorf("an unknown status: status %d, want 400", refused.Code)
	}
}

// Who is present in a room is not something an outsider may learn.
func TestRoomPresenceRequiresVisibility(t *testing.T) {
	handler := newTestAPI(t)
	ownerToken := completeSetup(t, handler)
	outsiderToken := registerAndSignIn(t, handler, "outsider")

	privateID := makeRoom(t, handler, ownerToken, "secret", "private")

	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+privateID+"/presence", nil, outsiderToken)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("an outsider reading presence: status %d, want 404", recorder.Code)
	}

	// The owner sees it.
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+privateID+"/presence", nil, ownerToken, http.StatusOK)
}

func TestTypingThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)
	ownerToken := completeSetup(t, handler)
	otherToken := registerAndSignIn(t, handler, "amina")

	roomID := makeRoom(t, handler, ownerToken, "general", "public")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, otherToken, http.StatusOK)

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/typing", nil, ownerToken, http.StatusNoContent)

	// The other person sees it.
	var typing []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/typing", nil, otherToken)
	if err := json.Unmarshal(recorder.Body.Bytes(), &typing); err != nil {
		t.Fatalf("decode typing: %v", err)
	}
	if len(typing) != 1 {
		t.Fatalf("%d people typing, want 1", len(typing))
	}

	// The typist is not shown their own indication.
	var own []map[string]any
	ownRecorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/typing", nil, ownerToken)
	if err := json.Unmarshal(ownRecorder.Body.Bytes(), &own); err != nil {
		t.Fatalf("decode own typing: %v", err)
	}
	if len(own) != 0 {
		t.Error("a person is shown their own typing indication")
	}
}
