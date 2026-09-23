package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

/*
Interactive messages beyond buttons: menus, fields, updating a message in
place, answering one person privately, and what a program remembers between
the steps of an exchange.
*/

// postInteractive posts a message with controls and returns its identifier.
func postInteractive(t *testing.T, handler http.Handler, token, roomID string, controls []map[string]any) string {
	t.Helper()

	recorder := postWithActions(t, handler, token, roomID, "Which environment?", controls)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("posting controls returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)["id"].(string)
}

// pressWith activates a control, carrying a value and a version.
func pressWith(t *testing.T, handler http.Handler, token, roomID, messageID, action string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/messages/"+url.PathEscape(messageID)+"/actions/"+action, body, token)
}

func updateControls(t *testing.T, handler http.Handler, token, roomID, messageID string, version int, controls []map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+url.PathEscape(messageID)+"/controls",
		map[string]any{"version": version, "body": "Deploying to staging.", "actions": controls}, token)
}

var environmentMenu = map[string]any{
	"name": "environment", "label": "Environment", "kind": "select",
	"options": []map[string]any{
		{"value": "staging", "label": "Staging"},
		{"value": "production", "label": "Production"},
	},
}

var reasonField = map[string]any{
	"name": "reason", "label": "Reason", "kind": "input", "placeholder": "Why?", "maxLength": 40,
}

// A message carrying a menu and a field is posted, and they come back as sent.
func TestAMessageMayCarryAMenuAndAField(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "deploys", "public")

	recorder := postWithActions(t, handler, owner, roomID, "Which environment?",
		[]map[string]any{environmentMenu, reasonField})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("a menu and a field were refused: %d %s", recorder.Code, recorder.Body.String())
	}

	var message struct {
		Content struct {
			Actions []struct {
				Kind    string `json:"kind"`
				Options []struct {
					Value string `json:"value"`
				} `json:"options"`
			} `json:"actions"`
		} `json:"content"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content.Actions) != 2 || message.Content.Actions[0].Kind != "select" ||
		len(message.Content.Actions[0].Options) != 2 || message.Content.Actions[1].Kind != "input" {
		t.Fatalf("the controls came back as %s", recorder.Body.String())
	}
}

// A menu with too many options, or a field with no name, is refused before it
// is posted — and the message is not in the channel afterwards.
func TestMalformedMenusAndFieldsAreRefusedBeforePosting(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "deploys", "public")

	options := make([]map[string]any, 0, 26)
	for i := range 26 {
		options = append(options, map[string]any{"value": fmt.Sprintf("v%d", i), "label": fmt.Sprintf("Option %d", i)})
	}

	refused := map[string]map[string]any{
		"Too many options": {"name": "pick", "label": "Pick", "kind": "select", "options": options},
		"A nameless field": {"name": "", "label": "Reason", "kind": "input"},
	}
	for body, control := range refused {
		recorder := postWithActions(t, handler, owner, roomID, body, []map[string]any{control})
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%q returned %d, want 400: %s", body, recorder.Code, recorder.Body.String())
		}

		timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, owner)
		if strings.Contains(timeline.Body.String(), body) {
			t.Errorf("%q was refused and is in the channel anyway", body)
		}
	}
}

// A choice from the menu reaches the program; one it never offered does not.
func TestAMenuPressCarriesOnlyAnOfferedChoice(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "deploys", "public")
	messageID := postInteractive(t, handler, owner, roomID, []map[string]any{environmentMenu, reasonField})

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	chosen := pressWith(t, handler, member, roomID, messageID, "environment", map[string]any{"value": "staging"})
	if chosen.Code != http.StatusCreated {
		t.Fatalf("choosing staging returned %d: %s", chosen.Code, chosen.Body.String())
	}
	content := decode(t, chosen)["content"].(map[string]any)
	if content["value"] != "staging" || content["action"] != "environment" {
		t.Errorf("the press event carried %v", content)
	}

	forged := pressWith(t, handler, member, roomID, messageID, "environment", map[string]any{"value": "the-database"})
	if forged.Code != http.StatusBadRequest {
		t.Errorf("a choice the menu never offered returned %d", forged.Code)
	}

	typed := pressWith(t, handler, member, roomID, messageID, "reason", map[string]any{"value": "Hotfix for login"})
	if typed.Code != http.StatusCreated {
		t.Errorf("typing a reason returned %d: %s", typed.Code, typed.Body.String())
	}
	tooLong := pressWith(t, handler, owner, roomID, messageID, "reason",
		map[string]any{"value": strings.Repeat("x", 41)})
	if tooLong.Code != http.StatusBadRequest {
		t.Errorf("text past the field's limit returned %d", tooLong.Code)
	}
}

/*
Two responses to the same press, racing, produce one update and one refusal.

Run concurrently and repeatedly rather than one after the other: sequential
calls would pass against an implementation that read the version, compared it
in Go and then wrote, which is exactly the race the database is there to close.
*/
func TestConcurrentUpdatesApplyOnceAndRefuseTheOther(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "deploys", "public")

	for round := range 5 {
		messageID := postInteractive(t, handler, owner, roomID, []map[string]any{environmentMenu})

		var wait sync.WaitGroup
		codes := make([]int, 2)
		start := make(chan struct{})
		for i := range codes {
			wait.Go(func() {
				<-start
				codes[i] = updateControls(t, handler, owner, roomID, messageID, 0,
					[]map[string]any{{"name": fmt.Sprintf("done-%d", i), "label": "Done"}}).Code
			})
		}
		close(start)
		wait.Wait()

		applied, stale := 0, 0
		for _, code := range codes {
			switch code {
			case http.StatusOK:
				applied++
			case http.StatusConflict:
				stale++
			}
		}
		if applied != 1 || stale != 1 {
			t.Fatalf("round %d: two racing updates returned %v, want one 200 and one 409", round, codes)
		}
	}
}

// Only the program that posted a message may replace what it asks.
func TestOnlyTheAuthorMayUpdateAMessage(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "deploys", "public")
	messageID := postInteractive(t, handler, owner, roomID, []map[string]any{environmentMenu})

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	if code := updateControls(t, handler, member, roomID, messageID, 0, nil).Code; code != http.StatusForbidden {
		t.Errorf("somebody else updating the message returned %d, want 403", code)
	}
}

/*
A press through a view whose controls have since been replaced is refused as
stale, and a press against the new version is accepted.
*/
func TestAPressAgainstReplacedControlsIsStale(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "deploys", "public")
	messageID := postInteractive(t, handler, owner, roomID, []map[string]any{
		{"name": "approve", "label": "Approve"},
	})

	updated := updateControls(t, handler, owner, roomID, messageID, 0, []map[string]any{
		{"name": "approve", "label": "Approve the rollback"},
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("the update returned %d: %s", updated.Code, updated.Body.String())
	}
	if version := decode(t, updated)["version"]; version != float64(1) {
		t.Fatalf("the update reported version %v, want 1", version)
	}

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	stale := pressWith(t, handler, member, roomID, messageID, "approve", map[string]any{"version": 0})
	if stale.Code != http.StatusConflict {
		t.Fatalf("a press through the replaced view returned %d, want 409: %s", stale.Code, stale.Body.String())
	}

	current := pressWith(t, handler, member, roomID, messageID, "approve", map[string]any{"version": 1})
	if current.Code != http.StatusCreated {
		t.Fatalf("a press against the current version returned %d: %s", current.Code, current.Body.String())
	}
	if decode(t, current)["content"].(map[string]any)["version"] != float64(1) {
		t.Error("the press event does not say which version it answered")
	}

	// And the room's log carries the update, so a reader arriving later sees
	// the new controls rather than the ones that were replaced.
	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, member)
	if !strings.Contains(timeline.Body.String(), "message.updated") ||
		!strings.Contains(timeline.Body.String(), "Approve the rollback") {
		t.Error("the timeline does not carry the update")
	}
}

// A control restricted to a permission refuses whoever does not hold it.
func TestARestrictedControlRefusesThoseWithoutThePermission(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "deploys", "public")
	messageID := postInteractive(t, handler, owner, roomID, []map[string]any{
		{"name": "approve", "label": "Approve", "requires": "moderation.review"},
	})

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	if code := pressWith(t, handler, member, roomID, messageID, "approve", nil).Code; code != http.StatusForbidden {
		t.Errorf("a member without the permission returned %d, want 403", code)
	}
	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, owner)
	if strings.Contains(timeline.Body.String(), "action.pressed") {
		t.Error("a refused press reached the room's log anyway")
	}

	// The administrator holds everything, so the same press goes through.
	if code := pressWith(t, handler, owner, roomID, messageID, "approve", nil).Code; code != http.StatusCreated {
		t.Errorf("somebody holding the permission returned %d", code)
	}
}

// Pressing faster than the limit is refused, saying when to try again.
func TestRapidPressesAreThrottled(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	setSetting(t, handler, owner, "integrations.presses_per_user_per_minute", "2")
	roomID := makeRoom(t, handler, owner, "deploys", "public")
	messageID := postInteractive(t, handler, owner, roomID, []map[string]any{
		{"name": "a", "label": "A"}, {"name": "b", "label": "B"}, {"name": "c", "label": "C"},
	})

	for _, action := range []string{"a", "b"} {
		if code := pressWith(t, handler, owner, roomID, messageID, action, nil).Code; code != http.StatusCreated {
			t.Fatalf("press %s within the limit returned %d", action, code)
		}
	}
	third := pressWith(t, handler, owner, roomID, messageID, "c", nil)
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("a press past the limit returned %d, want 429", third.Code)
	}
	if third.Header().Get("Retry-After") == "" {
		t.Error("the refusal does not say when to try again")
	}
}

// liveFrames opens a live connection as somebody and subscribes to a room.
func liveFrames(t *testing.T, server *httptest.Server, token, roomID string) (<-chan map[string]any, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		cancel()
		t.Fatalf("opening the live connection: %v", err)
	}

	frames := make(chan map[string]any, 64)
	subscribed := make(chan struct{})
	go func() {
		defer close(frames)
		for {
			_, data, err := socket.Read(ctx)
			if err != nil {
				return
			}
			var frame map[string]any
			if json.Unmarshal(data, &frame) != nil {
				continue
			}
			switch frame["type"] {
			case "ready":
				request, _ := json.Marshal(map[string]any{"action": "subscribe", "rooms": []string{roomID}})
				_ = socket.Write(ctx, websocket.MessageText, request)
			case "subscribed":
				close(subscribed)
			default:
				frames <- frame
			}
		}
	}()

	select {
	case <-subscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("the live connection never subscribed")
	}

	return frames, func() {
		_ = socket.Close(websocket.StatusNormalClosure, "")
		cancel()
	}
}

// nextFrame waits for a frame of a type, returning everything seen before it.
func nextFrame(t *testing.T, frames <-chan map[string]any, want string) (map[string]any, []map[string]any) {
	t.Helper()

	var before []map[string]any
	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame, open := <-frames:
			if !open {
				t.Fatalf("the connection closed before a %q frame arrived", want)
			}
			if frame["type"] == want {
				return frame, before
			}
			before = append(before, frame)
		case <-deadline:
			t.Fatalf("no %q frame arrived", want)
		}
	}
}

/*
A private answer reaches the person who pressed, and a second member's live
connection never receives it.

The second member's absence is checked against something they do receive: the
program posts publicly after answering privately, so by the time that public
message arrives on the second connection the private one would already have
arrived before it, if it were ever going to.
*/
func TestAPrivateReplyReachesOnlyThePresser(t *testing.T) {
	handler := newTestAPI(t)
	program := completeSetup(t, handler)
	roomID := makeRoom(t, handler, program, "deploys", "public")
	messageID := postInteractive(t, handler, program, roomID, []map[string]any{{"name": "approve", "label": "Approve"}})

	presser := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, presser, roomID)
	bystander := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, bystander, roomID)

	server := httptest.NewServer(handler)
	defer server.Close()

	presserFrames, closePresser := liveFrames(t, server, presser, roomID)
	defer closePresser()
	bystanderFrames, closeBystander := liveFrames(t, server, bystander, roomID)
	defer closeBystander()

	press := pressWith(t, handler, presser, roomID, messageID, "approve", nil)
	if press.Code != http.StatusCreated {
		t.Fatalf("the press returned %d", press.Code)
	}
	pressID := decode(t, press)["id"].(string)

	reply := call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/presses/"+url.PathEscape(pressID)+"/reply",
		map[string]any{"body": "Approved — only you can see this."}, program)
	if reply.Code != http.StatusCreated {
		t.Fatalf("the private reply returned %d: %s", reply.Code, reply.Body.String())
	}
	if delivered := decode(t, reply)["delivered"]; delivered != float64(1) {
		t.Errorf("the reply reports %v deliveries, want 1", delivered)
	}

	frame, _ := nextFrame(t, presserFrames, "private")
	payload := frame["payload"].(map[string]any)
	if payload["body"] != "Approved — only you can see this." || payload["press"] != pressID {
		t.Errorf("the presser received %v", payload)
	}

	postWithActions(t, handler, program, roomID, "Deploying now.", nil)
	for {
		// Every frame up to the public message is inspected: the press's own
		// event comes first, then — if it were ever going to — the private
		// reply, then the message.
		frame, before := nextFrame(t, bystanderFrames, "event")
		for _, seen := range before {
			if seen["type"] == "private" {
				t.Fatalf("a second member received a private reply: %v", seen)
			}
		}
		content, _ := frame["payload"].(map[string]any)["content"].(map[string]any)
		if content["body"] == "Deploying now." {
			break
		}
	}

	// And nobody but the message's author may answer the press privately.
	intruder := call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/presses/"+url.PathEscape(pressID)+"/reply",
		map[string]any{"body": "psst"}, bystander)
	if intruder.Code != http.StatusForbidden {
		t.Errorf("somebody else answering the press returned %d, want 403", intruder.Code)
	}

	// Nor can a private reply be aimed at anything but a press.
	notAPress := call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/presses/"+url.PathEscape(messageID)+"/reply",
		map[string]any{"body": "psst"}, program)
	if notAPress.Code != http.StatusNotFound {
		t.Errorf("a private reply to an ordinary message returned %d, want 404", notAPress.Code)
	}
}

/*
What a program remembers about one person is readable by that program and by
nobody else — not the person, not another program — and goes when the program
or the conversation does.
*/
func TestIntegrationContextIsPrivateAndDiscardedWithItsOwner(t *testing.T) {
	handler := newTestAPI(t)
	program := completeSetup(t, handler)
	roomID := makeRoom(t, handler, program, "deploys", "public")

	amina := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, amina, roomID)
	aminaID := userIDOf(t, handler, program, "amina")
	other := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, other, roomID)

	path := "/api/rooms/" + roomID + "/context/" + aminaID
	stored := call(t, handler, http.MethodPut, path,
		map[string]any{"state": map[string]any{"build": 41, "step": "chose-environment"}}, program)
	if stored.Code != http.StatusNoContent {
		t.Fatalf("remembering returned %d: %s", stored.Code, stored.Body.String())
	}

	read := call(t, handler, http.MethodGet, path, nil, program)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), "chose-environment") {
		t.Fatalf("the program reading its own context got %d: %s", read.Code, read.Body.String())
	}

	// Every other caller, through the same interface, finds nothing.
	for name, token := range map[string]string{"the person it is about": amina, "another member": other} {
		recorder := call(t, handler, http.MethodGet, path, nil, token)
		if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "chose-environment") {
			t.Errorf("%s read the program's context: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}

	// Removing the program discards what it knew.
	ctx := context.Background()
	var remembered int
	if _, err := poolForTest.Exec(ctx, "DELETE FROM users WHERE username = 'founder'"); err != nil {
		t.Fatalf("removing the program's account: %v", err)
	}
	if err := poolForTest.QueryRow(ctx, "SELECT count(*) FROM integration_context").Scan(&remembered); err != nil {
		t.Fatal(err)
	}
	if remembered != 0 {
		t.Errorf("%d context rows outlived the program they belonged to", remembered)
	}
}

// And the conversation's removal discards it too.
func TestIntegrationContextGoesWithItsConversation(t *testing.T) {
	handler := newTestAPI(t)
	program := completeSetup(t, handler)
	roomID := makeRoom(t, handler, program, "deploys", "public")
	amina := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, amina, roomID)
	aminaID := userIDOf(t, handler, program, "amina")

	if code := call(t, handler, http.MethodPut, "/api/rooms/"+roomID+"/context/"+aminaID,
		map[string]any{"state": map[string]any{"step": 1}}, program).Code; code != http.StatusNoContent {
		t.Fatalf("remembering returned %d", code)
	}

	ctx := context.Background()
	if _, err := poolForTest.Exec(ctx, "DELETE FROM rooms WHERE id = $1", roomID); err != nil {
		t.Fatalf("removing the conversation: %v", err)
	}
	var remembered int
	if err := poolForTest.QueryRow(ctx, "SELECT count(*) FROM integration_context").Scan(&remembered); err != nil {
		t.Fatal(err)
	}
	if remembered != 0 {
		t.Errorf("%d context rows outlived their conversation", remembered)
	}
}
