package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// press activates a button on a message.
func press(
	t *testing.T,
	handler http.Handler,
	token, roomID, eventID, action string,
) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/actions/"+action, nil, token)
}

// postAndGetID sends a message with buttons and returns its event identifier.
func postAndGetID(
	t *testing.T,
	handler http.Handler,
	token, roomID, body string,
	offered []map[string]any,
) string {
	t.Helper()
	recorder := postWithActions(t, handler, token, roomID, body, offered)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("posting: %d %s", recorder.Code, recorder.Body.String())
	}

	var message struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &message); err != nil {
		t.Fatalf("parsing the message: %v", err)
	}
	return message.ID
}

/*
A press reaches the program that asked the question.

It becomes an event in the room, which is how: over the live connection the
program already holds, through the same delivery every other event uses. Here
that is read from the timeline, which is the same log.
*/
func TestAPressReachesTheSender(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := postAndGetID(t, handler, owner, roomID, "Deploy?", []map[string]any{
		{"name": "deploy", "label": "Deploy"},
		{"name": "hold", "label": "Not yet"},
	})

	member := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, member, roomID)

	recorder := press(t, handler, member, roomID, eventID, "deploy")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("pressing returned %d: %s", recorder.Code, recorder.Body.String())
	}

	// The event says which button and on which message, and who pressed it.
	var pressed struct {
		Type    string `json:"type"`
		Sender  string `json:"sender"`
		Content struct {
			Action  string `json:"action"`
			Pressed string `json:"pressed"`
		} `json:"content"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &pressed); err != nil {
		t.Fatalf("parsing the press: %v", err)
	}

	if pressed.Type != "action.pressed" {
		t.Errorf("the press is a %q event", pressed.Type)
	}
	if pressed.Content.Action != "deploy" {
		t.Errorf("the press names action %q", pressed.Content.Action)
	}
	if pressed.Content.Pressed != eventID {
		t.Errorf("the press names message %q, want %q", pressed.Content.Pressed, eventID)
	}
	if !strings.HasPrefix(pressed.Sender, "juma@") {
		t.Errorf("the press is attributed to %q, not to who pressed it", pressed.Sender)
	}

	// And it is in the room's log, which is what a program reads.
	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, owner)
	if !strings.Contains(timeline.Body.String(), "action.pressed") {
		t.Error("the press is not in the timeline the sender reads")
	}
}

/*
A repeated press of a one-shot action is refused.

The verification this task asks for. A program receiving two events for one
decision cannot tell that from somebody changing their mind.
*/
func TestARepeatedPressOfAOneShotActionIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := postAndGetID(t, handler, owner, roomID, "Deploy?", []map[string]any{
		{"name": "deploy", "label": "Deploy", "once": true},
	})

	member := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, member, roomID)

	if first := press(t, handler, member, roomID, eventID, "deploy"); first.Code != http.StatusCreated {
		t.Fatalf("the first press returned %d: %s", first.Code, first.Body.String())
	}

	second := press(t, handler, member, roomID, eventID, "deploy")
	if second.Code != http.StatusConflict {
		t.Errorf("a second press of a one-shot action returned %d, want 409", second.Code)
	}

	/*
		And by somebody else, which is what "once" means: one press in total,
		not one each. A vote offered once to a room is answered once.
	*/
	other := registerAndSignIn(t, handler, "neema")
	joinRoom(t, handler, other, roomID)

	byOther := press(t, handler, other, roomID, eventID, "deploy")
	if byOther.Code != http.StatusConflict {
		t.Errorf("a one-shot action was pressed by a second person: %d", byOther.Code)
	}
}

/*
An ordinary action may be pressed by different people, once each.

The counterpart: without this, "once" would be indistinguishable from the
default and the flag would mean nothing.
*/
func TestAnOrdinaryActionIsPressedOncePerPerson(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := postAndGetID(t, handler, owner, roomID, "Coming?", []map[string]any{
		{"name": "yes", "label": "Yes"},
	})

	member := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, member, roomID)
	other := registerAndSignIn(t, handler, "neema")
	joinRoom(t, handler, other, roomID)

	if first := press(t, handler, member, roomID, eventID, "yes"); first.Code != http.StatusCreated {
		t.Fatalf("the first person's press returned %d", first.Code)
	}
	if second := press(t, handler, other, roomID, eventID, "yes"); second.Code != http.StatusCreated {
		t.Errorf("a second person answering the same question returned %d", second.Code)
	}

	// But not twice by the same person: a repeat is a retry or a mistake, and
	// neither should count twice.
	if again := press(t, handler, member, roomID, eventID, "yes"); again.Code != http.StatusConflict {
		t.Errorf("the same person answered twice: %d", again.Code)
	}
}

/*
A press naming a button the message does not offer is refused.

Accepting it would let anybody send a program any action name they liked, which
is the same as letting them call its handlers directly.
*/
func TestPressingAButtonThatIsNotThereIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := postAndGetID(t, handler, owner, roomID, "Deploy?", []map[string]any{
		{"name": "deploy", "label": "Deploy"},
	})

	if recorder := press(t, handler, owner, roomID, eventID, "drop-database"); recorder.Code != http.StatusNotFound {
		t.Errorf("pressing a button that does not exist returned %d", recorder.Code)
	}

	// And on a message offering nothing at all.
	plain := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "just talking"}, owner)
	var message struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(plain.Body.Bytes(), &message)

	if recorder := press(t, handler, owner, roomID, message.ID, "deploy"); recorder.Code != http.StatusNotFound {
		t.Errorf("pressing a button on a message with none returned %d", recorder.Code)
	}
}

// An expired action is refused, and says so distinctly from a missing one.
func TestPressingAnExpiredActionIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	eventID := postAndGetID(t, handler, owner, roomID, "Vote now", []map[string]any{
		{"name": "vote", "label": "Vote", "expires": past},
	})

	if recorder := press(t, handler, owner, roomID, eventID, "vote"); recorder.Code != http.StatusGone {
		t.Errorf("pressing a button that expired an hour ago returned %d, want 410", recorder.Code)
	}
}

/*
Somebody who is not in the channel cannot press.

Answering a question in a channel is taking part in it, so it needs membership
rather than the ability to read — a public channel is readable by everybody on
the server.
*/
func TestPressingNeedsMembership(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := postAndGetID(t, handler, owner, roomID, "Deploy?", []map[string]any{
		{"name": "deploy", "label": "Deploy"},
	})

	outsider := registerAndSignIn(t, handler, "juma")

	if recorder := press(t, handler, outsider, roomID, eventID, "deploy"); recorder.Code == http.StatusCreated {
		t.Error("somebody who is not in the channel answered its question")
	}
}
