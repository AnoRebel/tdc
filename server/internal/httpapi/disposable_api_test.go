package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// makeDisposable creates a chat whose messages expire after a lifetime.
func makeDisposable(t *testing.T, handler http.Handler, token, slug string, seconds int) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]any{
		// Public because these tests are about expiry, not visibility, and a
		// private room cannot be joined by somebody who was not invited.
		"slug": slug, "name": slug, "visibility": "public",
		"kind": "disposable", "messageTtlSeconds": seconds,
	}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("creating a disposable chat returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	var room struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &room); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if room.Kind != "disposable" {
		t.Fatalf("the room came back as %q, expected disposable", room.Kind)
	}
	return room.ID
}

/*
TestADisposableChatNeedsALifetime is what stops the label being a lie.

Without one it is an ordinary channel that says its messages disappear, and
every participant would have been shown a promise the room does not keep.
*/
func TestADisposableChatNeedsALifetime(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	/*
		Slugs long enough to be valid.

		An earlier draft used "a" and "b", which are refused for being too
		short — so the test passed with this rule removed entirely, reporting
		success for a refusal that had nothing to do with lifetimes.
	*/
	for _, body := range []map[string]any{
		{"slug": "hakuna-muda", "name": "Hakuna muda", "kind": "disposable"},
		{"slug": "muda-sifuri", "name": "Muda sifuri", "kind": "disposable",
			"messageTtlSeconds": 0},
	} {
		recorder := call(t, handler, http.MethodPost, "/api/rooms", body, owner)

		if recorder.Code == http.StatusCreated {
			t.Errorf("a disposable chat was created with no lifetime: %v", body)
		}
		// And refused for the right reason, not merely refused.
		if !strings.Contains(recorder.Body.String(), "how long its messages last") {
			t.Errorf("%v was refused with %q, which is not about the lifetime",
				body, truncate(recorder.Body.String()))
		}
	}
}

/*
TestADisposableChatDisclosesItsLifetime is the scenario the spec names.

Every participant has to be told before they post. A conversation that quietly
deletes itself is one people say things in they would not have said knowing.
*/
func TestADisposableChatDisclosesItsLifetime(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeDisposable(t, handler, owner, "fupi", 3600)

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	// Read by an ordinary participant, not only whoever made it.
	room := decode(t, call(t, handler, http.MethodGet, "/api/rooms/"+roomID, nil, member))

	if seconds, _ := room["retentionSeconds"].(float64); int(seconds) != 3600 {
		t.Errorf("the chat reports retentionSeconds %v, expected 3600", room["retentionSeconds"])
	}
	if fromChannel, _ := room["retentionFromChannel"].(bool); !fromChannel {
		t.Error("a disposable chat does not say the lifetime is its own")
	}
	if kind, _ := room["kind"].(string); kind != "disposable" {
		t.Errorf("the chat reports kind %q, expected disposable", kind)
	}
}

/*
TestExpiredContentIsNotServedToAClientThatWasOfflineThroughout is the property
the requirement turns on.

Hiding expired content in a client is not expiry: a client that was not running
never got the instruction, and one written by somebody else never had it. What
the server holds has to be gone, so that what any client asks for comes back
empty however long it was away.
*/
func TestExpiredContentIsNotServedToAClientThatWasOfflineThroughout(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeDisposable(t, handler, owner, "fupi", 3600)

	sendMessage(t, handler, owner, roomID, "Siri yangu")

	// The lifetime passes while nobody is connected.
	ageMessages(t, roomID, "2 hours")

	removed, err := services.Moderation.ExpireDue(context.Background(), 0, 100, nil)
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if removed == 0 {
		t.Fatal("nothing expired in a disposable chat past its lifetime")
	}

	/*
		Now a client asks for everything, as one returning after a week does.
		The server has nothing to give it — not a message it is asked to hide,
		but no content at all.
	*/
	response := call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=100", nil, owner)
	if response.Code != http.StatusOK {
		t.Fatalf("reading the timeline returned %d", response.Code)
	}

	if strings.Contains(response.Body.String(), "Siri yangu") {
		t.Errorf("expired content was served to a client that was offline "+
			"throughout: %s", truncate(response.Body.String()))
	}
}

/*
TestAnExpiredMessageLeavesAPlaceholderDisclosingNothing covers what is left
behind.

The event stays so the ordering and the signed chain around it stay
verifiable — but what a reader sees must say only that something was here, not
what. A placeholder carrying the sender's words would expire nothing.
*/
func TestAnExpiredMessageLeavesAPlaceholderDisclosingNothing(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeDisposable(t, handler, owner, "fupi", 3600)

	sendMessage(t, handler, owner, roomID, "Siri yangu")
	ageMessages(t, roomID, "2 hours")

	if _, err := services.Moderation.ExpireDue(context.Background(), 0, 100, nil); err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}

	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=100", nil, owner))

	found := false
	for _, event := range timeline {
		kind, _ := event["type"].(string)
		if kind != "message.sent" {
			continue
		}
		found = true

		if redacted, _ := event["redacted"].(bool); !redacted {
			t.Error("an expired message is not marked as gone, so a client " +
				"cannot tell it from one that is merely empty")
		}
		if body := bodyOfEvent(event); body != "" {
			t.Errorf("the placeholder still carries content: %q", body)
		}
	}

	if !found {
		/*
			The event itself must remain. Removing the row would leave a gap in
			a sequence whose whole value is having none, and a client
			resynchronising would see history it cannot verify.
		*/
		t.Error("the expired message is gone entirely, leaving a hole in the " +
			"event sequence rather than a placeholder")
	}
}

/*
TestAnOrdinaryChannelIsNotDisposable keeps the two apart.

A channel with no lifetime keeps its messages, and nothing about adding
disposable chats should change that.
*/
func TestAnOrdinaryChannelIsNotDisposable(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	sendMessage(t, handler, owner, roomID, "Ujumbe wa kawaida")
	ageMessages(t, roomID, "400 days")

	if _, err := services.Moderation.ExpireDue(context.Background(), 0, 100, nil); err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}

	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=100", nil, owner))

	kept := false
	for _, event := range timeline {
		if strings.Contains(bodyOfEvent(event), "Ujumbe wa kawaida") {
			kept = true
		}
	}
	if !kept {
		t.Error("an ordinary channel lost content with no policy configured")
	}
}

// TestAnUnknownKindIsRefused keeps a typo from silently making a channel.
func TestAnUnknownKindIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]any{
		"slug": "c", "name": "c", "kind": "disposible", // deliberately misspelt
		"messageTtlSeconds": 3600,
	}, owner)

	if recorder.Code == http.StatusCreated {
		t.Error("a misspelt kind created a room, which would silently be a channel")
	}
}
