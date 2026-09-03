package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// makeRoom creates a room through the API and returns its identifier.
func makeRoom(t *testing.T, handler http.Handler, token, slug, visibility string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]string{
		"slug": slug, "name": slug, "visibility": visibility,
	}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create room %q: status %d, body %s", slug, recorder.Code, recorder.Body.String())
	}

	id, _ := decode(t, recorder)["id"].(string)
	if id == "" {
		t.Fatal("the created room has no identifier")
	}
	return id
}

// registerAndSignIn creates an ordinary account and returns its token.
func registerAndSignIn(t *testing.T, handler http.Handler, username string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": username, "password": testPassword,
	}, "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register %q: status %d, body %s", username, recorder.Code, recorder.Body.String())
	}
	return signIn(t, handler, username, testPassword)
}

func TestRoomLifecycleThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	roomID := makeRoom(t, handler, token, "general", "public")

	// The creator is a member, so the client can tell a room it may post in
	// from one it must join.
	room := decode(t, call(t, handler, http.MethodGet, "/api/rooms/"+roomID, nil, token))
	if room["member"] != true {
		t.Error("the creator is not reported as a member")
	}
	if address, _ := room["address"].(string); !strings.Contains(address, "@") {
		t.Errorf("address = %q, want it to name the home server", address)
	}

	// Creation appears in the timeline, so the log is complete from the start.
	var timeline []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if len(timeline) != 1 || timeline[0]["type"] != "room.created" {
		t.Errorf("timeline = %v, want the creation event", timeline)
	}
}

func TestSendAndReadMessages(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "hello everyone"}, token, http.StatusCreated))

	if sent["sequence"] == nil {
		t.Error("the message has no sequence number, so gaps cannot be detected")
	}

	var timeline []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}

	found := false
	for _, event := range timeline {
		if event["id"] == sent["id"] {
			found = true
		}
	}
	if !found {
		t.Error("the sent message is not in the timeline")
	}
}

// Resending with the same token must not post twice.
func TestRetryDoesNotPostTwice(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	body := map[string]string{"body": "only once", "clientToken": "retry-token"}

	first := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		body, token, http.StatusCreated))
	second := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		body, token, http.StatusCreated))

	if first["id"] != second["id"] {
		t.Error("a retry created a second message")
	}
}

// A private room must be indistinguishable from one that does not exist.
func TestPrivateRoomIsNotDisclosedByTheApi(t *testing.T) {
	handler := newTestAPI(t)
	ownerToken := completeSetup(t, handler)
	outsiderToken := registerAndSignIn(t, handler, "outsider")

	privateID := makeRoom(t, handler, ownerToken, "secret", "private")

	// Fetching it directly reports it missing, not forbidden.
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+privateID, nil, outsiderToken)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("fetching a private room: status %d, want 404", recorder.Code)
	}

	// A room that genuinely does not exist answers identically.
	missing := call(t, handler, http.MethodGet,
		"/api/rooms/00000000-0000-0000-0000-000000000000", nil, outsiderToken)
	if missing.Code != recorder.Code || missing.Body.String() != recorder.Body.String() {
		t.Errorf("a private room is distinguishable from a missing one:\n private: %d %s\n missing: %d %s",
			recorder.Code, recorder.Body.String(), missing.Code, missing.Body.String())
	}

	// It must not appear in the room list.
	var listed []map[string]any
	listRecorder := call(t, handler, http.MethodGet, "/api/rooms", nil, outsiderToken)
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode rooms: %v", err)
	}
	for _, room := range listed {
		if room["id"] == privateID {
			t.Error("a private room appeared in an outsider's room list")
		}
	}

	// Joining must not confirm it either.
	joinRecorder := call(t, handler, http.MethodPost, "/api/rooms/"+privateID+"/join", nil, outsiderToken)
	if joinRecorder.Code != http.StatusNotFound {
		t.Errorf("joining a private room: status %d, want 404", joinRecorder.Code)
	}

	// Nor may its timeline be read.
	timelineRecorder := call(t, handler, http.MethodGet, "/api/rooms/"+privateID+"/timeline", nil, outsiderToken)
	if timelineRecorder.Code != http.StatusNotFound {
		t.Errorf("reading a private timeline: status %d, want 404", timelineRecorder.Code)
	}
}

// Posting requires membership, not merely visibility.
func TestPostingRequiresMembership(t *testing.T) {
	handler := newTestAPI(t)
	ownerToken := completeSetup(t, handler)
	otherToken := registerAndSignIn(t, handler, "newcomer")

	roomID := makeRoom(t, handler, ownerToken, "general", "public")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "hello"}, otherToken)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("posting without joining: status %d, want 403", recorder.Code)
	}

	if joined := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, otherToken); joined.Code != http.StatusOK {
		t.Fatalf("join: status %d, body %s", joined.Code, joined.Body.String())
	}

	after := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "hello"}, otherToken)
	if after.Code != http.StatusCreated {
		t.Errorf("posting after joining: status %d, body %s", after.Code, after.Body.String())
	}
}

func TestEditAndDeleteThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "frist post"}, token, http.StatusCreated))
	messageID, _ := sent["id"].(string)

	edit := decode(t, mustCall(t, handler, http.MethodPatch,
		"/api/rooms/"+roomID+"/messages/"+messageID,
		map[string]string{"body": "first post"}, token, http.StatusCreated))
	if edit["id"] == messageID {
		t.Error("the edit replaced the original event rather than adding one")
	}
	if edit["type"] != "message.edited" {
		t.Errorf("edit type = %v, want message.edited", edit["type"])
	}

	deletion := decode(t, mustCall(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/messages/"+messageID, nil, token, http.StatusCreated))
	if deletion["type"] != "message.deleted" {
		t.Errorf("deletion type = %v, want message.deleted", deletion["type"])
	}

	// The original stays in the timeline, redacted, so the conversation does
	// not appear never to have contained it.
	var timeline []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}

	var original map[string]any
	for _, event := range timeline {
		if event["id"] == messageID {
			original = event
		}
	}
	if original == nil {
		t.Fatal("the deleted message vanished from the timeline entirely")
	}
	if original["redacted"] != true {
		t.Error("the deleted message is not marked redacted")
	}
}

// One person must not be able to edit another's message.
func TestCannotEditAnothersMessage(t *testing.T) {
	handler := newTestAPI(t)
	ownerToken := completeSetup(t, handler)
	otherToken := registerAndSignIn(t, handler, "someone")

	roomID := makeRoom(t, handler, ownerToken, "general", "public")
	if joined := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, otherToken); joined.Code != http.StatusOK {
		t.Fatalf("join: status %d", joined.Code)
	}

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "mine"}, ownerToken, http.StatusCreated))
	messageID, _ := sent["id"].(string)

	recorder := call(t, handler, http.MethodPatch,
		"/api/rooms/"+roomID+"/messages/"+messageID,
		map[string]string{"body": "not yours"}, otherToken)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", recorder.Code)
	}
}

// Creating a room requires the permission, so an ordinary member cannot.
func TestCreatingARoomRequiresPermission(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)
	ordinary := registerAndSignIn(t, handler, "ordinary")

	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]string{
		"slug": "unauthorised", "name": "Unauthorised",
	}, ordinary)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "channel.create") {
		t.Errorf("the refusal does not name the permission: %s", recorder.Body.String())
	}
}

func TestRoomRoutesRequireAuthentication(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/rooms"},
		{http.MethodPost, "/api/rooms"},
		{http.MethodGet, "/api/rooms/" + roomID},
		{http.MethodGet, "/api/rooms/" + roomID + "/timeline"},
		{http.MethodPost, "/api/rooms/" + roomID + "/messages"},
		{http.MethodGet, "/api/live"},
	} {
		recorder := call(t, handler, route.method, route.path, nil, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: status %d, want 401",
				route.method, route.path, recorder.Code)
		}
	}
}
