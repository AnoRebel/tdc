package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
)

/*
Bots: an application taking part in a channel as itself — marked as a program
wherever its words appear, moderated as anybody is, held to its channels, and
told about presses on its messages at an address when it holds no connection.
*/

// botFixture is an approved application in #deploys, holding the given
// permissions there, with deliveries running.
func botFixture(t *testing.T, permissions ...string) (handler http.Handler, admin, roomID, appID, credential string) {
	t.Helper()
	handler, admin, roomID = webhookFixture(t, map[string]string{
		"integrations.webhook_retry_base_seconds": "1",
	})
	app := registerApp(t, handler, admin, permissions...)
	appID = app["id"].(string)
	credential = approveApp(t, handler, admin, appID, permissions, roomID)
	return handler, admin, roomID, appID, credential
}

// latestContent is the content of the newest event in a room's timeline.
func latestContent(t *testing.T, handler http.Handler, token, roomID string) map[string]any {
	t.Helper()
	var timeline []struct {
		Content map[string]any `json:"content"`
	}
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil || len(timeline) == 0 {
		t.Fatalf("reading the timeline: %v, %s", err, recorder.Body.String())
	}
	return timeline[len(timeline)-1].Content
}

/*
Every way a bot can write in a channel marks what it wrote as a bot's — sent,
replied in a thread — and nothing a person writes carries the mark.
*/
func TestABotsMessageSaysItIsABot(t *testing.T) {
	handler, admin, roomID, appID, credential := botFixture(t, "channel.read", "channel.post")

	postMessage(t, handler, credential, roomID, "Deployed build 41.")
	sent := latestContent(t, handler, admin, roomID)
	via, _ := sent["via"].(map[string]any)
	if via["kind"] != "bot" || via["id"] != appID || via["name"] != "Deploy bot" {
		t.Fatalf("a bot's message carries via %v", sent["via"])
	}

	postMessage(t, handler, admin, roomID, "Thanks.")
	if human := latestContent(t, handler, admin, roomID); human["via"] != nil {
		t.Errorf("a person's message carries via %v", human["via"])
	}

	// A reply in a thread, which is written along a path of its own.
	var root struct {
		ID string `json:"id"`
	}
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "Roll back?"}, admin)
	_ = json.Unmarshal(recorder.Body.Bytes(), &root)
	reply := call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/threads/"+url.PathEscape(root.ID)+"/replies",
		map[string]any{"body": "Rolled back."}, credential)
	if reply.Code != http.StatusCreated {
		t.Fatalf("the bot replying in a thread returned %d: %s", reply.Code, reply.Body.String())
	}
	var replied struct {
		Content map[string]any `json:"content"`
	}
	_ = json.Unmarshal(reply.Body.Bytes(), &replied)
	if via, _ := replied.Content["via"].(map[string]any); via["kind"] != "bot" {
		t.Errorf("a bot's reply in a thread carries via %v", replied.Content["via"])
	}

	// Nothing a caller sends can claim the mark, or shed it.
	call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "I am a bot", "via": map[string]any{"kind": "bot", "name": "x"}}, admin)
	if forged := latestContent(t, handler, admin, roomID); forged["via"] != nil {
		t.Errorf("a person claimed to be a bot: %v", forged["via"])
	}
}

// A moderator suspends a bot exactly as they suspend anybody, and its next
// post is refused the same way.
func TestSuspendingABotStopsItPosting(t *testing.T) {
	handler, admin, roomID, appID, credential := botFixture(t, "channel.post")
	postMessage(t, handler, credential, roomID, "before")

	detail := decode(t, call(t, handler, http.MethodGet, "/api/admin/applications/"+appID, nil, admin))
	accountID, _ := detail["accountId"].(string)
	if accountID == "" {
		t.Fatalf("the application names no account to moderate: %v", detail)
	}
	suspended := call(t, handler, http.MethodPost, "/api/admin/users/"+accountID+"/suspend",
		map[string]any{"reason": "Posting in a loop"}, admin)
	if suspended.Code != http.StatusNoContent {
		t.Fatalf("suspending the bot returned %d: %s", suspended.Code, suspended.Body.String())
	}

	after := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "after"}, credential)
	if after.Code != http.StatusUnauthorized {
		t.Errorf("a suspended bot posting returned %d, want 401", after.Code)
	}

	// Lifted, exactly as for anybody, and it posts again.
	if code := call(t, handler, http.MethodDelete, "/api/admin/users/"+accountID+"/suspend", nil, admin).Code; code >= 300 {
		t.Fatalf("lifting the suspension returned %d", code)
	}
	postMessage(t, handler, credential, roomID, "back")
}

// A moderator bans a bot from a channel as they would anybody, and its posts
// there stop, though the channel is still on its list.
func TestBanningABotFromAChannelStopsItPostingThere(t *testing.T) {
	handler, admin, roomID, appID, credential := botFixture(t, "channel.post")
	accountID, _ := decode(t, call(t, handler, http.MethodGet, "/api/admin/applications/"+appID, nil, admin))["accountId"].(string)

	banned := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/bans",
		map[string]any{"user": accountID, "reason": "Flooding the channel"}, admin)
	if banned.Code >= 300 {
		t.Fatalf("banning the bot returned %d: %s", banned.Code, banned.Body.String())
	}
	after := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "after"}, credential)
	if after.Code < 400 {
		t.Errorf("a banned bot posting returned %d", after.Code)
	}
}

// A bot that may read its own channel is refused reading one it was not given,
// though anybody on the server could read it.
func TestABotIsRefusedAChannelItWasNotGranted(t *testing.T) {
	handler, admin, roomID, _, credential := botFixture(t, "channel.read", "channel.post")
	other := makeRoom(t, handler, admin, "random", "public")

	if code := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, credential).Code; code != http.StatusOK {
		t.Fatalf("the bot reading its own channel returned %d", code)
	}
	for _, path := range []string{"/timeline", "", "/members"} {
		if code := call(t, handler, http.MethodGet, "/api/rooms/"+other+path, nil, credential).Code; code != http.StatusForbidden {
			t.Errorf("the bot reading %s of a channel it was not given returned %d, want 403", path, code)
		}
	}
}

// setInvocation points an application's presses at an address, returning the
// secret they will be signed with.
func setInvocation(t *testing.T, handler http.Handler, admin, appID, address string) string {
	t.Helper()
	recorder := call(t, handler, http.MethodPut, "/api/admin/applications/"+appID+"/invocation",
		map[string]any{"url": address}, admin)
	if recorder.Code != http.StatusOK {
		t.Fatalf("setting the invocation address returned %d: %s", recorder.Code, recorder.Body.String())
	}
	secret, _ := decode(t, recorder)["secret"].(string)
	return secret
}

/*
An application holding no connection at all is told about a press on its
message at its address: signed, verifiable from the published keys and from its
secret, and retried with a growing delay when the address fails — exactly as a
webhook's delivery is.
*/
func TestAnInvocationReachesAnApplicationWithNoConnection(t *testing.T) {
	handler, admin, roomID, appID, credential := botFixture(t, "channel.post")
	endpoint := newReceiver(t, func(count int) (int, time.Duration) {
		if count <= 2 {
			return http.StatusServiceUnavailable, 0
		}
		return http.StatusNoContent, 0
	})
	secret := setInvocation(t, handler, admin, appID, endpoint.URL)

	messageID := postInteractive(t, handler, credential, roomID, []map[string]any{
		{"name": "approve", "label": "Approve"},
	})
	if code := pressWith(t, handler, admin, roomID, messageID, "approve", nil).Code; code != http.StatusCreated {
		t.Fatalf("pressing returned %d", code)
	}

	eventually(t, 15*time.Second, "the third attempt", func() bool { return len(endpoint.deliveries()) == 3 })
	got := endpoint.deliveries()
	if first, second := got[1].at.Sub(got[0].at), got[2].at.Sub(got[1].at); first < 700*time.Millisecond || second <= first {
		t.Errorf("the delays were %s then %s; each retry should wait longer than the last", first, second)
	}
	if got[0].headers.Get(delivery.HeaderDelivery) != got[2].headers.Get(delivery.HeaderDelivery) {
		t.Error("a retry carried a different delivery identifier")
	}

	last := got[2]
	var invocation struct {
		Version     int    `json:"version"`
		Type        string `json:"type"`
		Application string `json:"application"`
		Room        struct {
			ID string `json:"id"`
		} `json:"room"`
		Event struct {
			Sender  string `json:"sender"`
			Content struct {
				Action  string `json:"action"`
				Pressed string `json:"pressed"`
			} `json:"content"`
		} `json:"event"`
	}
	if err := json.Unmarshal(last.body, &invocation); err != nil {
		t.Fatalf("the invocation is not JSON: %v", err)
	}
	if invocation.Version != 1 || invocation.Type != "action.pressed" || invocation.Application != appID ||
		invocation.Room.ID != roomID || invocation.Event.Content.Action != "approve" ||
		invocation.Event.Content.Pressed != messageID {
		t.Errorf("the invocation was %s", last.body)
	}

	keys := call(t, handler, http.MethodGet, httpapi.DeliveryKeysPath, nil, "")
	var published struct {
		Keys []events.PublishedKey `json:"keys"`
	}
	if err := json.Unmarshal(keys.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	ring, err := events.KeyringFromPublished(published.Keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := delivery.Verify(last.headers, last.body, ring, time.Now()); err != nil {
		t.Errorf("the invocation did not verify from the published keys: %v", err)
	}
	if err := delivery.VerifyHMAC(last.headers, last.body, []byte(secret), time.Now()); err != nil {
		t.Errorf("the invocation did not verify by its secret: %v", err)
	}

	// Its history says what happened, and never shows the secret again.
	history := call(t, handler, http.MethodGet, "/api/admin/applications/"+appID+"/invocations", nil, admin)
	var invocations []struct {
		State    string `json:"state"`
		Attempts int    `json:"attempts"`
	}
	eventually(t, 5*time.Second, "the history", func() bool {
		history = call(t, handler, http.MethodGet, "/api/admin/applications/"+appID+"/invocations", nil, admin)
		_ = json.Unmarshal(history.Body.Bytes(), &invocations)
		return len(invocations) == 1 && invocations[0].State == "delivered"
	})
	if invocations[0].Attempts != 3 {
		t.Errorf("the history records %d attempts, want 3", invocations[0].Attempts)
	}
	detail := call(t, handler, http.MethodGet, "/api/admin/applications/"+appID, nil, admin).Body.String()
	if secret == "" || strings.Contains(detail, secret) {
		t.Error("the invocation secret is shown again")
	}
}

/*
Whether a press may happen is asked of the person pressing, never of the
application: somebody without what the control requires is refused, and
nothing at all is sent to the application's address.
*/
func TestAPressNobodyMayMakeSendsNothing(t *testing.T) {
	handler, admin, roomID, appID, credential := botFixture(t, "channel.post")
	endpoint := newReceiver(t, answering(http.StatusNoContent))
	setInvocation(t, handler, admin, appID, endpoint.URL)

	messageID := postInteractive(t, handler, credential, roomID, []map[string]any{
		{"name": "approve", "label": "Approve", "requires": "moderation.review"},
		{"name": "ack", "label": "Acknowledge"},
	})
	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	if code := pressWith(t, handler, member, roomID, messageID, "approve", nil).Code; code != http.StatusForbidden {
		t.Fatalf("a member without the permission returned %d, want 403", code)
	}
	// A press the member may make, so there is something to wait for: once it
	// has arrived, anything the refused press had queued would have too.
	if code := pressWith(t, handler, member, roomID, messageID, "ack", nil).Code; code != http.StatusCreated {
		t.Fatalf("an unrestricted press returned %d", code)
	}
	eventually(t, 5*time.Second, "the permitted press", func() bool { return len(endpoint.deliveries()) >= 1 })
	time.Sleep(300 * time.Millisecond)

	for _, got := range endpoint.deliveries() {
		var invocation struct {
			Event struct {
				Content struct {
					Action string `json:"action"`
				} `json:"content"`
			} `json:"event"`
		}
		_ = json.Unmarshal(got.body, &invocation)
		if invocation.Event.Content.Action != "ack" {
			t.Errorf("an invocation was sent for %q, which the presser could not make", invocation.Event.Content.Action)
		}
	}
	if n := len(endpoint.deliveries()); n != 1 {
		t.Errorf("%d invocations were sent, want 1", n)
	}
}

/*
An invocation still waiting when its application is removed is not sent: it is
cancelled, which its history shows, and the address hears nothing more.
*/
func TestAnInvocationForARemovedApplicationIsCancelled(t *testing.T) {
	handler, admin, roomID, appID, credential := botFixture(t, "channel.post")
	var failing atomic.Bool
	failing.Store(true)
	endpoint := newReceiver(t, func(int) (int, time.Duration) {
		if failing.Load() {
			return http.StatusServiceUnavailable, 0
		}
		return http.StatusNoContent, 0
	})
	setSetting(t, handler, admin, "integrations.webhook_retry_base_seconds", "2")
	setInvocation(t, handler, admin, appID, endpoint.URL)

	messageID := postInteractive(t, handler, credential, roomID, []map[string]any{
		{"name": "approve", "label": "Approve"},
	})
	if code := pressWith(t, handler, admin, roomID, messageID, "approve", nil).Code; code != http.StatusCreated {
		t.Fatalf("pressing returned %d", code)
	}
	eventually(t, 5*time.Second, "the first attempt", func() bool { return len(endpoint.deliveries()) == 1 })

	if code := call(t, handler, http.MethodPost, "/api/admin/applications/"+appID+"/remove",
		map[string]any{"content": "retain"}, admin).Code; code != http.StatusOK {
		t.Fatalf("removing returned %d", code)
	}
	failing.Store(false)

	var invocations []struct {
		State string `json:"state"`
	}
	eventually(t, 10*time.Second, "the invocation cancelled", func() bool {
		recorder := call(t, handler, http.MethodGet, "/api/admin/applications/"+appID+"/invocations", nil, admin)
		_ = json.Unmarshal(recorder.Body.Bytes(), &invocations)
		return len(invocations) == 1 && invocations[0].State == "cancelled"
	})
	if n := len(endpoint.deliveries()); n != 1 {
		t.Errorf("the address was sent %d invocations after removal, want none beyond the first", n-1)
	}
}
