package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
Messages that offer choices.

A program posts a message with buttons; somebody presses one; the program finds
out. What these check is the boundary — that the server refuses, at the point
the message is posted, anything asking a client to do something nobody agreed
to.
*/

// postWithActions sends a message carrying buttons and returns the response.
func postWithActions(
	t *testing.T,
	handler http.Handler,
	token, roomID, body string,
	offered []map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": body, "actions": offered}, token)
}

/*
A message may carry labelled buttons, and they reach the people reading it.

The ordinary case, and the one everything else is a refusal of.
*/
func TestAMessageMayCarryButtons(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	recorder := postWithActions(t, handler, owner, roomID, "Ship it?", []map[string]any{
		{"name": "approve", "label": "Approve"},
		{"name": "reject", "label": "Reject"},
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("a message with two buttons was refused: %d %s",
			recorder.Code, recorder.Body.String())
	}

	var message struct {
		Content struct {
			Body    string `json:"body"`
			Actions []struct {
				Name  string `json:"name"`
				Label string `json:"label"`
			} `json:"actions"`
		} `json:"content"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &message); err != nil {
		t.Fatalf("parsing the message: %v", err)
	}

	if len(message.Content.Actions) != 2 {
		t.Fatalf("the message came back with %d buttons, want 2", len(message.Content.Actions))
	}
	if message.Content.Actions[0].Label != "Approve" {
		t.Errorf("the first button reads %q", message.Content.Actions[0].Label)
	}
	if message.Content.Body != "Ship it?" {
		t.Errorf("the message body is %q", message.Content.Body)
	}
}

/*
An action naming a capability this server does not declare is refused when the
message is posted.

The verification this task asks for. A message that reached a channel and then
failed on being pressed has already been read, and somebody has already decided
the button was worth trusting.
*/
func TestAnUndeclaredCapabilityIsRefusedWhenPosted(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	recorder := postWithActions(t, handler, owner, roomID, "Run this", []map[string]any{
		{"name": "run", "label": "Run it", "capability": "execute"},
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a button asking clients to execute something returned %d: %s",
			recorder.Code, recorder.Body.String())
	}

	// The reason is said, because the caller is a program and a bare refusal
	// is an afternoon of guessing.
	if body := recorder.Body.String(); !strings.Contains(body, "execute") {
		t.Errorf("the refusal does not name what was refused: %s", body)
	}
}

// And the message is not posted. A refusal that left the message in the
// channel would be the failure this exists to prevent.
func TestARefusedMessageIsNotPosted(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	postWithActions(t, handler, owner, roomID, "Run this", []map[string]any{
		{"name": "run", "label": "Run it", "capability": "execute"},
	})

	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, owner)
	if strings.Contains(timeline.Body.String(), "Run this") {
		t.Error("a message whose buttons were refused is in the channel anyway")
	}
}

/*
A client is asked to open only what it should.

A javascript: link in a message anybody can post is a way to run something in
every reader's client.
*/
func TestAClientIsNotAskedToOpenAnythingDangerous(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	for _, link := range []string{"javascript:alert(1)", "file:///etc/passwd"} {
		recorder := postWithActions(t, handler, owner, roomID, "Have a look", []map[string]any{
			{"name": "look", "label": "Look", "capability": "open", "url": link},
		})
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("a button opening %q returned %d", link, recorder.Code)
		}
	}

	// And the one that is allowed.
	ok := postWithActions(t, handler, owner, roomID, "Read this", []map[string]any{
		{"name": "docs", "label": "Read the docs", "capability": "open",
			"url": "https://example.test/docs"},
	})
	if ok.Code != http.StatusCreated {
		t.Errorf("a button opening an https link returned %d: %s", ok.Code, ok.Body.String())
	}
}

// An ordinary message is unchanged: no actions key, nothing extra to parse.
func TestAnOrdinaryMessageCarriesNoActions(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "just talking"}, owner)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("an ordinary message returned %d", recorder.Code)
	}

	if strings.Contains(recorder.Body.String(), `"actions"`) {
		t.Error("an ordinary message carries an actions key it does not need")
	}
}

/*
A program posts one, which is the point of the phase.

Everything the web client does is an HTTP call somebody else can make, and
offering somebody a choice is one of the things worth making.
*/
func TestAProgramCanOfferAChoice(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	token, _ := issueToken(t, handler, owner, "deploy bot",
		[]string{"channel.post", "channel.read"})

	recorder := postWithActions(t, handler, token, roomID, "Deploy to production?", []map[string]any{
		{"name": "deploy", "label": "Deploy", "once": true},
		{"name": "hold", "label": "Not yet"},
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("a program offering a choice was refused: %d %s",
			recorder.Code, recorder.Body.String())
	}
}
