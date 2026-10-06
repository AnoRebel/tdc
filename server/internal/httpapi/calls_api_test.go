package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// withCalls is a server with calling switched on and a channel two members are in.
func withCalls(t *testing.T) (http.Handler, string, string, string, string) {
	t.Helper()
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	setFeature(t, handler, admin, "calls", true)
	room := makeRoom(t, handler, admin, "standup", "public")
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")
	for _, person := range []string{amina, baraka} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, person, http.StatusOK)
	}
	return handler, admin, room, amina, baraka
}

func participantsOf(call map[string]any) []string {
	names := []string{}
	for _, raw := range call["participants"].([]any) {
		names = append(names, raw.(map[string]any)["username"].(string))
	}
	slices.Sort(names)
	return names
}

// Off by default, and off means absent: nothing answers, nothing is offered.
func TestCallsAreAbsentUntilSwitchedOn(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	room := makeRoom(t, handler, admin, "standup", "public")

	for _, attempt := range []struct{ method, path string }{
		{http.MethodGet, "/api/rooms/" + room + "/call"},
		{http.MethodPost, "/api/rooms/" + room + "/call"},
		{http.MethodGet, "/api/calls/ice"},
	} {
		if code := call(t, handler, attempt.method, attempt.path, map[string]any{}, admin).Code; code != http.StatusNotFound {
			t.Errorf("%s %s with calling off: status %d, want 404", attempt.method, attempt.path, code)
		}
	}
	if slices.Contains(advertised(t, handler), "calls") {
		t.Error("calling is advertised while it is off")
	}
}

/*
A call starts, others join, and the conversation records that it happened.

The start and the end are in the conversation's log, so somebody reading back
sees there was a call and how long it lasted; who was in it is not.
*/
func TestACallStartsIsJoinedAndEnds(t *testing.T) {
	handler, _, room, amina, baraka := withCalls(t)

	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, amina, http.StatusNotFound)

	started := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call",
		map[string]any{"audio": true, "video": true}, amina, http.StatusOK))
	joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call",
		map[string]any{"audio": true}, baraka, http.StatusOK))
	if started["id"] != joined["id"] {
		t.Fatal("the second person started a second call instead of joining the first")
	}
	if got := participantsOf(joined); !slices.Equal(got, []string{"amina", "baraka"}) {
		t.Errorf("the call holds %v", got)
	}

	muted := decode(t, mustCall(t, handler, http.MethodPatch, "/api/rooms/"+room+"/call",
		map[string]any{"audio": false, "video": false}, amina, http.StatusOK))
	for _, raw := range muted["participants"].([]any) {
		if p := raw.(map[string]any); p["username"] == "amina" && p["audio"] != false {
			t.Error("muting is not shown to the others")
		}
	}

	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/call", nil, amina, http.StatusNoContent)
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, baraka, http.StatusOK)
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/call", nil, baraka, http.StatusNoContent)
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, baraka, http.StatusNotFound)

	types := timelineTypes(t, handler, amina, room)
	if !slices.Contains(types, "call.started") || !slices.Contains(types, "call.ended") {
		t.Errorf("the conversation's log reads %v; the call's start and end are missing", types)
	}
}

/*
Calling is its own permission: an operator can leave somebody able to post and
unable to call, and the refusal names what was missing.
*/
func TestCallingIsSeparableFromPosting(t *testing.T) {
	handler, admin, room, amina, baraka := withCalls(t)

	withdraw(t, handler, admin, "call.start", room)
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, amina)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "call.start") {
		t.Errorf("starting without call.start: %d %s", recorder.Code, recorder.Body.String())
	}
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "still here"}, amina, http.StatusCreated)

	// The administrator is not held to Member, so starts one; joining is its
	// own permission again.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, admin, http.StatusOK)
	withdraw(t, handler, admin, "call.join", room)
	recorder = call(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, baraka)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "call.join") {
		t.Errorf("joining without call.join: %d %s", recorder.Code, recorder.Body.String())
	}

	// And somebody outside the conversation cannot join at all.
	outsider := registerAndSignIn(t, handler, "chausiku")
	private := makeRoom(t, handler, admin, "board", "private")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+private+"/call", map[string]any{"audio": true}, admin, http.StatusOK)
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+private+"/call", map[string]any{"audio": true}, outsider).Code; code == http.StatusOK {
		t.Error("somebody outside a private conversation joined its call")
	}
}

// The participant limit refuses the next person and says why.
func TestAFullCallRefusesTheNextPerson(t *testing.T) {
	handler, admin, room, amina, baraka := withCalls(t)
	setSetting(t, handler, admin, "calls.max_participants", "1")

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, amina, http.StatusOK)
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, baraka)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "full") {
		t.Errorf("joining a full call: %d %s", recorder.Code, recorder.Body.String())
	}
}

/*
Somebody whose connection went without saying so leaves the call.

Nobody in these tests holds a live connection, so the sweep takes everybody out
and the call ends — what happens when a laptop lid closes mid-call.
*/
func TestTheSweepEndsACallNobodyIsConnectedTo(t *testing.T) {
	handler, _, room, amina, _ := withCalls(t)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, amina, http.StatusOK)

	serverOf(handler).SweepCalls(context.Background())

	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, amina, http.StatusNotFound)
	if !slices.Contains(timelineTypes(t, handler, amina, room), "call.ended") {
		t.Error("a call the sweep emptied was not recorded as ended")
	}
}

// liveConnection opens the live connection as somebody and waits until it is ready.
func liveConnection(t *testing.T, ctx context.Context, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatalf("opening the live connection: %v", err)
	}
	t.Cleanup(func() { _ = socket.Close(websocket.StatusNormalClosure, "") })
	for {
		_, data, err := socket.Read(ctx)
		if err != nil {
			t.Fatalf("the connection ended before it was ready: %v", err)
		}
		if strings.Contains(string(data), `"type":"ready"`) {
			return socket
		}
	}
}

// nextCallFrame reads frames until one of the given type, or fails at the deadline.
func nextCallFrame(t *testing.T, ctx context.Context, socket *websocket.Conn, frameType string) (map[string]any, bool) {
	t.Helper()
	for {
		_, data, err := socket.Read(ctx)
		if err != nil {
			return nil, false
		}
		var frame map[string]any
		if json.Unmarshal(data, &frame) == nil && frame["type"] == frameType {
			return frame, true
		}
	}
}

/*
Negotiation passes between participants, and only between them.

An offer from one participant reaches the other. Somebody in the conversation
but not in the call cannot send one to a participant — signalling is not a way
into a call — and is told so without the participant hearing anything.
*/
func TestSignallingPassesOnlyBetweenParticipants(t *testing.T) {
	handler, admin, room, amina, baraka := withCalls(t)
	outsider := registerAndSignIn(t, handler, "chausiku")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, outsider, http.StatusOK)
	_ = admin

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	aminaLive := liveConnection(t, ctx, server, amina)
	barakaLive := liveConnection(t, ctx, server, baraka)
	outsiderLive := liveConnection(t, ctx, server, outsider)

	started := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, amina, http.StatusOK))
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, baraka, http.StatusOK)
	callID := started["id"].(string)
	barakaID := accountID(t, handler, baraka)

	send := func(socket *websocket.Conn, to, body string) {
		message, _ := json.Marshal(map[string]any{
			"action": "signal", "call": callID, "to": to, "signal": map[string]string{"sdp": body},
		})
		if err := socket.Write(ctx, websocket.MessageText, message); err != nil {
			t.Fatalf("sending a signal: %v", err)
		}
	}

	// The outsider first, so anything it got through would be read before
	// amina's offer below.
	send(outsiderLive, barakaID, "from outside")
	if _, refused := nextCallFrame(t, ctx, outsiderLive, "error"); !refused {
		t.Error("somebody not in the call was not told their signal was refused")
	}

	send(aminaLive, barakaID, "an offer")
	frame, ok := nextCallFrame(t, ctx, barakaLive, "call.signal")
	if !ok {
		t.Fatal("a participant's offer never reached the other participant")
	}
	payload, _ := json.Marshal(frame["payload"])
	if strings.Contains(string(payload), "from outside") {
		t.Error("a signal from somebody not in the call reached a participant")
	}
	if !strings.Contains(string(payload), "an offer") {
		t.Errorf("the participant received %s", payload)
	}
}

// Holding a call while in another shows the others, and coming back clears it.
func TestHoldingACallIsShownToTheOthers(t *testing.T) {
	handler, _, room, amina, baraka := withCalls(t)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, amina, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, baraka, http.StatusOK)

	heldOf := func(call map[string]any) any {
		for _, raw := range call["participants"].([]any) {
			if p := raw.(map[string]any); p["username"] == "amina" {
				return p["held"]
			}
		}
		return nil
	}
	held := decode(t, mustCall(t, handler, http.MethodPatch, "/api/rooms/"+room+"/call",
		map[string]any{"audio": true, "held": true}, amina, http.StatusOK))
	if heldOf(held) != true {
		t.Errorf("holding is not shown: %v", held)
	}
	back := decode(t, mustCall(t, handler, http.MethodPatch, "/api/rooms/"+room+"/call",
		map[string]any{"audio": true}, amina, http.StatusOK))
	if heldOf(back) != false {
		t.Errorf("returning to the call still shows it held: %v", back)
	}
}
