package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
Incoming webhooks: an address an outside system posts into one channel through,
built on the assumption that it will leak.
*/

// createIncoming makes an incoming webhook as somebody allowed to, returning
// the creation response.
func createIncoming(t *testing.T, handler http.Handler, token, roomID, label string) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/incoming-webhooks",
		map[string]any{"label": label}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("creating an incoming webhook returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

func postThrough(t *testing.T, handler http.Handler, credential string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, handler, http.MethodPost, "/api/hooks/"+credential, body, "")
}

// timelineEvents reads a channel's timeline as a list of events.
func timelineEvents(t *testing.T, handler http.Handler, token, roomID string) []map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token)
	var events []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &events); err != nil {
		t.Fatalf("decoding the timeline: %v: %s", err, recorder.Body.String())
	}
	return events
}

/*
A post arrives in the bound channel, attributed to the webhook — its label, its
kind, and an address that is nobody's account.
*/
func TestAnIncomingWebhookPostsAsItself(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "builds", "public")
	created := createIncoming(t, handler, owner, roomID, "Build server")

	if !strings.HasPrefix(created["token"].(string), "tdc_hook_") ||
		!strings.HasSuffix(created["url"].(string), "/api/hooks/"+created["token"].(string)) {
		t.Fatalf("the credential or its address is not what the guide promises: %v", created)
	}

	posted := postThrough(t, handler, created["token"].(string), map[string]any{"text": "Build 41 **passed**"})
	if posted.Code != http.StatusCreated {
		t.Fatalf("posting through the webhook returned %d: %s", posted.Code, posted.Body.String())
	}

	var found map[string]any
	for _, event := range timelineEvents(t, handler, owner, roomID) {
		if content, _ := event["content"].(map[string]any); content["body"] == "Build 41 **passed**" {
			found = event
		}
	}
	if found == nil {
		t.Fatal("the message is not in the channel")
	}
	via, _ := found["content"].(map[string]any)["via"].(map[string]any)
	if via["kind"] != "webhook" || via["name"] != "Build server" || via["id"] != created["id"] {
		t.Errorf("the message is attributed as %v", via)
	}
	if sender := found["sender"].(string); !strings.HasPrefix(sender, "webhook-") || strings.HasPrefix(sender, "founder") {
		t.Errorf("the message's sender is %q, which reads as a person", sender)
	}

	// The credential is not recoverable from the listing.
	listing := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/incoming-webhooks", nil, owner)
	if strings.Contains(listing.Body.String(), created["token"].(string)) {
		t.Error("the listing shows the credential again")
	}
	if !strings.Contains(listing.Body.String(), `"posted":1`) {
		t.Errorf("the listing does not count the post: %s", listing.Body.String())
	}
}

// Naming any other channel is refused, and nothing is posted anywhere.
func TestAnIncomingWebhookCannotPostElsewhere(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	bound := makeRoom(t, handler, owner, "builds", "public")
	other := makeRoom(t, handler, owner, "random", "public")
	credential := createIncoming(t, handler, owner, bound, "CI")["token"].(string)

	for _, naming := range []map[string]any{
		{"text": "sneaky", "room": other},
		{"text": "sneaky", "channel": "random"},
		{"text": "sneaky", "channel": "#random"},
	} {
		if code := postThrough(t, handler, credential, naming).Code; code != http.StatusForbidden {
			t.Errorf("naming another channel %v returned %d, want 403", naming, code)
		}
	}
	for _, roomID := range []string{bound, other} {
		for _, event := range timelineEvents(t, handler, owner, roomID) {
			if content, _ := event["content"].(map[string]any); content["body"] == "sneaky" {
				t.Errorf("a refused post reached %s", roomID)
			}
		}
	}

	// Naming its own channel, by slug, is fine.
	if code := postThrough(t, handler, credential, map[string]any{"text": "fine", "channel": "#builds"}).Code; code != http.StatusCreated {
		t.Errorf("naming its own channel returned %d", code)
	}
}

// A revoked webhook's very next call is refused, with nothing in between.
func TestARevokedIncomingWebhookIsRefusedAtOnce(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "builds", "public")
	created := createIncoming(t, handler, owner, roomID, "CI")
	credential := created["token"].(string)

	if code := postThrough(t, handler, credential, map[string]any{"text": "before"}).Code; code != http.StatusCreated {
		t.Fatalf("posting before revocation returned %d", code)
	}
	revoked := call(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/incoming-webhooks/"+created["id"].(string), nil, owner)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoking returned %d", revoked.Code)
	}
	if code := postThrough(t, handler, credential, map[string]any{"text": "after"}).Code; code != http.StatusNotFound {
		t.Errorf("the next call after revocation returned %d, want 404", code)
	}
}

/*
What a webhook posts goes through the same filter a person's message does, and
is stored the same way — so it is rendered the same way, by the same code, with
the same protection against markup.
*/
func TestIncomingContentIsTreatedAsAPersonsIs(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	configureFilter(t, handler, owner, "darn", "mask")
	roomID := makeRoom(t, handler, owner, "builds", "public")
	credential := createIncoming(t, handler, owner, roomID, "CI")["token"].(string)

	hostile := `<script>alert("x")</script><img src=x onerror=alert(1)> darn it`
	postMessage(t, handler, owner, roomID, hostile)
	if code := postThrough(t, handler, credential, map[string]any{"body": hostile}).Code; code != http.StatusCreated {
		t.Fatalf("posting through the webhook returned %d", code)
	}

	var bodies []string
	for _, event := range timelineEvents(t, handler, owner, roomID) {
		if content, _ := event["content"].(map[string]any); content["body"] != nil && event["type"] == "message.sent" {
			bodies = append(bodies, content["body"].(string))
		}
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("the person's and the webhook's messages were stored differently: %q", bodies)
	}
	if strings.Contains(bodies[1], "darn") {
		t.Error("the operator's filter did not apply to the webhook")
	}
}

// Past its rate a webhook is refused with when to retry, and its owner can see
// that calls were refused.
func TestAnIncomingWebhookIsRateLimitedVisibly(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	setSetting(t, handler, owner, "integrations.incoming_webhook_posts_per_minute", "2")
	roomID := makeRoom(t, handler, owner, "builds", "public")
	credential := createIncoming(t, handler, owner, roomID, "CI")["token"].(string)

	for i := range 2 {
		if code := postThrough(t, handler, credential, map[string]any{"text": "ok"}).Code; code != http.StatusCreated {
			t.Fatalf("post %d within the limit returned %d", i, code)
		}
	}
	refused := postThrough(t, handler, credential, map[string]any{"text": "too many"})
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("a post past the limit returned %d", refused.Code)
	}
	if refused.Header().Get("Retry-After") == "" || decode(t, refused)["retryAfterSeconds"] == nil {
		t.Errorf("the refusal does not say when to retry: %s", refused.Body.String())
	}
	for _, event := range timelineEvents(t, handler, owner, roomID) {
		if content, _ := event["content"].(map[string]any); content["body"] == "too many" {
			t.Error("a refused post was posted anyway")
		}
	}

	listing := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/incoming-webhooks", nil, owner).Body.String()
	if !strings.Contains(listing, `"refused":1`) || !strings.Contains(listing, "lastRefused") {
		t.Errorf("the owner cannot see the refusal: %s", listing)
	}
}

// Creating and revoking are both in the audit log, with who did it.
func TestIncomingWebhooksAreAudited(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "builds", "public")
	created := createIncoming(t, handler, owner, roomID, "CI")
	call(t, handler, http.MethodDelete, "/api/rooms/"+roomID+"/incoming-webhooks/"+created["id"].(string), nil, owner)

	var entries []struct {
		Action   string `json:"action"`
		TargetID string `json:"targetId"`
		Actor    string `json:"actor"`
	}
	if err := json.Unmarshal(call(t, handler, http.MethodGet, "/api/admin/audit", nil, owner).Body.Bytes(), &entries); err != nil {
		t.Fatalf("decoding the audit log: %v", err)
	}
	seen := map[string]string{}
	for _, entry := range entries {
		if entry.TargetID == created["id"] {
			seen[entry.Action] = entry.Actor
		}
	}
	for _, action := range []string{"integration.incoming_webhook.created", "integration.incoming_webhook.revoked"} {
		if seen[action] != "founder" {
			t.Errorf("%s recorded with actor %q", action, seen[action])
		}
	}
	// And the credential is nowhere in the log.
	if strings.Contains(call(t, handler, http.MethodGet, "/api/admin/audit", nil, owner).Body.String(),
		created["token"].(string)) {
		t.Error("the audit log holds the credential")
	}
}

// Managing a channel's webhooks needs the permission; being in it does not.
func TestManagingIncomingWebhooksNeedsThePermission(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "builds", "public")
	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		recorder := call(t, handler, method, "/api/rooms/"+roomID+"/incoming-webhooks",
			map[string]any{"label": "mine"}, member)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s by a member without the permission returned %d", method, recorder.Code)
		}
	}
}

// Only link buttons: a webhook cannot answer a press, so it may not ask one.
func TestAnIncomingWebhookMayOfferOnlyLinks(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "builds", "public")
	credential := createIncoming(t, handler, owner, roomID, "CI")["token"].(string)

	question := postThrough(t, handler, credential, map[string]any{
		"text": "Deploy?", "actions": []map[string]any{{"name": "go", "label": "Deploy"}},
	})
	if question.Code != http.StatusBadRequest {
		t.Errorf("a button asking a question returned %d", question.Code)
	}
	link := postThrough(t, handler, credential, map[string]any{
		"text": "Build 41 failed", "actions": []map[string]any{
			{"name": "log", "label": "See the log", "capability": "open", "url": "https://ci.example.org/41"},
		},
	})
	if link.Code != http.StatusCreated {
		t.Errorf("a link button returned %d: %s", link.Code, link.Body.String())
	}
}
