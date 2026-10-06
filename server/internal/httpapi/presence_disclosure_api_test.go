package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// presenceOf returns one person's raw entry from a room's presence.
func presenceOf(t *testing.T, handler http.Handler, token, roomID, userID string) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/presence", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("room presence returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var entries []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatalf("parsing room presence: %v", err)
	}
	for _, entry := range entries {
		if entry["userId"] == userID {
			return entry
		}
	}
	t.Fatalf("%s is not in the room's presence", userID)
	return nil
}

/*
A room's presence keeps the same three choices the member list does.

The member list was where the choices were first applied, and the presence a
conversation polls for its online count is a second answer to the same
questions. A detail somebody withheld has to be absent from both: a client that
reads it from the one the member list does not cover has been told it, whatever
the member list says.
*/
func TestRoomPresenceLeavesOutWhatSomebodyWithholds(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, juma, roomID)
	joinRoom(t, handler, amina, roomID)

	mustCall(t, handler, http.MethodPut, "/api/me/status",
		map[string]any{"status": "busy", "text": "in a meeting"}, juma, http.StatusOK)
	jumaID := memberNamed(t, handler, amina, roomID, "juma")["id"].(string)

	// Each setting alone, then all three: each removes its own fields and
	// nothing else.
	for _, tc := range []struct {
		name    string
		privacy map[string]any
		absent  []string
		present []string
	}{
		{"discloses everything",
			map[string]any{"showsPresence": true, "showsStatus": true, "showsLastSeen": true},
			nil, []string{"state", "status", "statusText", "lastSeenAt"}},
		{"withholds presence",
			map[string]any{"showsPresence": false, "showsStatus": true, "showsLastSeen": true},
			[]string{"state"}, []string{"status", "statusText", "lastSeenAt"}},
		{"withholds status",
			map[string]any{"showsPresence": true, "showsStatus": false, "showsLastSeen": true},
			[]string{"status", "statusText"}, []string{"state", "lastSeenAt"}},
		{"withholds last seen",
			map[string]any{"showsPresence": true, "showsStatus": true, "showsLastSeen": false},
			[]string{"lastSeenAt"}, []string{"state", "status", "statusText"}},
		{"withholds all three",
			map[string]any{"showsPresence": false, "showsStatus": false, "showsLastSeen": false},
			[]string{"state", "status", "statusText", "lastSeenAt"}, nil},
	} {
		setPrivacy(t, handler, juma, tc.privacy)
		entry := presenceOf(t, handler, amina, roomID, jumaID)
		for _, field := range tc.absent {
			if value, present := entry[field]; present {
				t.Errorf("%s: room presence carries %s=%v", tc.name, field, value)
			}
		}
		for _, field := range tc.present {
			if _, present := entry[field]; !present {
				t.Errorf("%s: room presence lacks %s, which was not withheld", tc.name, field)
			}
		}
	}

	// Somebody's own entry is theirs to see whatever they withhold from others.
	setPrivacy(t, handler, juma, map[string]any{"showsPresence": false, "showsStatus": false, "showsLastSeen": false})
	own := presenceOf(t, handler, juma, roomID, jumaID)
	for _, field := range []string{"state", "status"} {
		if _, present := own[field]; !present {
			t.Errorf("juma's own presence lacks %s", field)
		}
	}
}
