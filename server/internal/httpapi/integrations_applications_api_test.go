package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

/*
Applications: registered, approved with exactly what they asked for, held to
their channels, visible to the people they can see, and removed as a whole.
*/

func registerApp(t *testing.T, handler http.Handler, token string, permissions ...string) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/admin/applications", map[string]any{
		"name": "Deploy bot", "purpose": "Posts deployment results and asks for approvals.",
		"permissions": permissions,
	}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("registering returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

// approveApp approves with what was asked for, in the channels given, and
// returns the application's credential.
func approveApp(t *testing.T, handler http.Handler, token, id string, permissions []string, channels ...string) string {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/admin/applications/"+id+"/approve",
		map[string]any{"permissions": permissions, "channels": channels}, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("approving returned %d: %s", recorder.Code, recorder.Body.String())
	}
	credential, _ := decode(t, recorder)["credential"].(map[string]any)
	secret, _ := credential["token"].(string)
	if !strings.HasPrefix(secret, "tdc_") {
		t.Fatalf("approval returned no credential: %v", credential)
	}
	return secret
}

// A registered application has nothing to act with: no account, no credential,
// and none can be issued until it is approved.
func TestARegisteredApplicationCanDoNothing(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	app := registerApp(t, handler, admin, "channel.post")
	id := app["id"].(string)

	if app["state"] != "registered" || app["account"] != nil {
		t.Errorf("a registered application has state %v and account %v", app["state"], app["account"])
	}
	if code := call(t, handler, http.MethodPost, "/api/admin/applications/"+id+"/credentials", nil, admin).Code; code != http.StatusConflict {
		t.Errorf("issuing a credential before approval returned %d, want 409", code)
	}
	detail := decode(t, call(t, handler, http.MethodGet, "/api/admin/applications/"+id, nil, admin))
	if detail["credentials"] != nil {
		t.Errorf("a registered application holds credentials: %v", detail["credentials"])
	}
}

/*
Approval is of exactly what the approver was shown: approving a different list
is refused, and the approver must hold everything they grant.
*/
func TestApprovalIsOfExactlyWhatWasShown(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	roomID := makeRoom(t, handler, admin, "deploys", "public")
	app := registerApp(t, handler, admin, "channel.read", "channel.post")
	id := app["id"].(string)

	// What an approver is shown, before approving.
	requested := decode(t, call(t, handler, http.MethodGet, "/api/admin/applications/"+id, nil, admin))["requested"]
	if raw, _ := json.Marshal(requested); string(raw) != `["channel.post","channel.read"]` {
		t.Fatalf("the approver is shown %s", raw)
	}

	for _, wrong := range [][]string{{"channel.post"}, {"channel.post", "channel.read", "server.administer"}} {
		recorder := call(t, handler, http.MethodPost, "/api/admin/applications/"+id+"/approve",
			map[string]any{"permissions": wrong, "channels": []string{roomID}}, admin)
		if recorder.Code != http.StatusConflict {
			t.Errorf("approving %v returned %d, want 409", wrong, recorder.Code)
		}
	}
	approveApp(t, handler, admin, id, []string{"channel.read", "channel.post"}, roomID)
}

// Somebody allowed to manage applications cannot grant one what they do not
// hold themselves.
func TestAnApproverCannotGrantWhatTheyDoNotHold(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	roomID := makeRoom(t, handler, admin, "deploys", "public")

	role := decode(t, call(t, handler, http.MethodPost, "/api/admin/roles",
		map[string]any{"name": "integrators", "description": "Installs applications"}, admin))
	roleID := role["id"].(string)
	if code := call(t, handler, http.MethodPut, "/api/admin/roles/"+roleID+"/permissions",
		map[string]any{"permission": "server.applications.manage", "granted": true}, admin).Code; code >= 300 {
		t.Fatalf("granting the role returned %d", code)
	}
	manager := registerAndSignIn(t, handler, "amina")
	if code := call(t, handler, http.MethodPost, "/api/admin/roles/"+roleID+"/members",
		map[string]any{"user": userIDOf(t, handler, admin, "amina")}, admin).Code; code >= 300 {
		t.Fatalf("assigning the role returned %d", code)
	}

	app := registerApp(t, handler, manager, "moderation.review")
	recorder := call(t, handler, http.MethodPost, "/api/admin/applications/"+app["id"].(string)+"/approve",
		map[string]any{"permissions": []string{"moderation.review"}, "channels": []string{roomID}}, manager)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("granting a permission the approver lacks returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

/*
An application approved by an administrator to post can post — in its channel —
and can do nothing an administrator can, and nothing anywhere else.
*/
func TestAnApplicationHoldsItsGrantAndNothingMore(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	allowed := makeRoom(t, handler, admin, "deploys", "public")
	other := makeRoom(t, handler, admin, "random", "public")
	app := registerApp(t, handler, admin, "channel.post")
	credential := approveApp(t, handler, admin, app["id"].(string), []string{"channel.post"}, allowed)

	posted := call(t, handler, http.MethodPost, "/api/rooms/"+allowed+"/messages",
		map[string]any{"body": "Deployed build 41."}, credential)
	if posted.Code != http.StatusCreated {
		t.Fatalf("posting in its channel returned %d: %s", posted.Code, posted.Body.String())
	}

	for _, attempt := range []struct {
		what, method, path string
		body               any
	}{
		{"post in another channel", http.MethodPost, "/api/rooms/" + other + "/messages", map[string]any{"body": "hi"}},
		{"join another channel", http.MethodPost, "/api/rooms/" + other + "/join", nil},
		{"read its own channel without channel.read", http.MethodGet, "/api/rooms/" + allowed + "/timeline", nil},
		{"read the audit log", http.MethodGet, "/api/admin/audit", nil},
		{"list people", http.MethodGet, "/api/admin/users", nil},
		{"create a channel", http.MethodPost, "/api/rooms", map[string]any{"slug": "x", "name": "x"}},
		{"issue itself a token", http.MethodPost, "/api/me/tokens", map[string]any{"label": "x", "scope": []string{}}},
		{"approve applications", http.MethodGet, "/api/admin/applications", nil},
	} {
		recorder := call(t, handler, attempt.method, attempt.path, attempt.body, credential)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("an application allowed only to post could %s: %d %s",
				attempt.what, recorder.Code, recorder.Body.String())
		}
	}
	if strings.Contains(call(t, handler, http.MethodGet, "/api/rooms/"+other+"/timeline", nil, admin).Body.String(), "hi") {
		t.Error("the refused post reached the other channel")
	}

	/*
		And a conversation it was put in by somebody else is still not one of
		its channels. Anybody can open a direct conversation with its account,
		which makes the account a member — so membership cannot be what decides
		where an application may act.
	*/
	account := decode(t, call(t, handler, http.MethodGet, "/api/admin/applications/"+app["id"].(string), nil, admin))["account"].(string)
	accountID := userIDOf(t, handler, admin, strings.Split(account, "@")[0])
	direct := call(t, handler, http.MethodPost, "/api/direct", map[string]any{"userIds": []string{accountID}}, admin)
	if direct.Code >= 300 {
		t.Fatalf("opening a direct conversation with the application returned %d: %s", direct.Code, direct.Body.String())
	}
	directID := decode(t, direct)["id"].(string)
	for _, attempt := range []struct{ method, path string }{
		{http.MethodPost, "/api/rooms/" + directID + "/messages"},
		{http.MethodGet, "/api/rooms/" + directID + "/timeline"},
	} {
		if code := call(t, handler, attempt.method, attempt.path, map[string]any{"body": "psst"}, credential).Code; code != http.StatusForbidden {
			t.Errorf("%s %s in a conversation it was put in returned %d", attempt.method, attempt.path, code)
		}
	}
}

// Another credential can be issued alongside the first, and revoking either
// leaves the other working — which is how one is rotated without a gap.
func TestAnApplicationCredentialRotatesWithoutAGap(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	roomID := makeRoom(t, handler, admin, "deploys", "public")
	app := registerApp(t, handler, admin, "channel.post")
	id := app["id"].(string)
	first := approveApp(t, handler, admin, id, []string{"channel.post"}, roomID)

	issued := decode(t, call(t, handler, http.MethodPost, "/api/admin/applications/"+id+"/credentials", nil, admin))
	second := issued["token"].(string)

	for _, credential := range []string{first, second} {
		if code := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
			map[string]any{"body": "both work"}, credential).Code; code != http.StatusCreated {
			t.Fatalf("a live credential returned %d", code)
		}
	}

	detail := decode(t, call(t, handler, http.MethodGet, "/api/admin/applications/"+id, nil, admin))
	var oldest string
	for _, entry := range detail["credentials"].([]any) {
		if entry.(map[string]any)["id"] != issued["id"] {
			oldest = entry.(map[string]any)["id"].(string)
		}
	}
	if code := call(t, handler, http.MethodDelete, "/api/admin/applications/"+id+"/credentials/"+oldest, nil, admin).Code; code != http.StatusNoContent {
		t.Fatalf("revoking the old credential returned %d", code)
	}

	if code := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "old"}, first).Code; code != http.StatusUnauthorized {
		t.Errorf("the revoked credential returned %d, want 401", code)
	}
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "new"}, second).Code; code != http.StatusCreated {
		t.Errorf("the new credential stopped working: %d", code)
	}
}

/*
An application making requests faster than its limit is throttled, while a
person's requests in the same moment are answered as usual.
*/
func TestAMisbehavingApplicationIsThrottledAlone(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	setSetting(t, handler, admin, "integrations.application_requests_per_minute", "5")
	roomID := makeRoom(t, handler, admin, "deploys", "public")
	app := registerApp(t, handler, admin, "channel.post")
	credential := approveApp(t, handler, admin, app["id"].(string), []string{"channel.post"}, roomID)

	throttled := 0
	for range 20 {
		if call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
			map[string]any{"body": "spam"}, credential).Code == http.StatusTooManyRequests {
			throttled++
		}

		started := time.Now()
		if code := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, admin).Code; code != http.StatusOK {
			t.Fatalf("a person's request returned %d while the application was throttled", code)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Errorf("a person's request took %s while the application was throttled", elapsed)
		}
	}
	if throttled != 15 {
		t.Errorf("%d of 20 requests were throttled; the limit is 5", throttled)
	}
	detail := decode(t, call(t, handler, http.MethodGet, "/api/admin/applications/"+app["id"].(string), nil, admin))
	if detail["rateLimited"].(float64) != 15 {
		t.Errorf("the application records %v throttled requests", detail["rateLimited"])
	}
}

// Anybody can see what is installed, and a channel's members can find which
// applications can act there — without administrator access.
func TestApplicationsAreVisibleToThePeopleTheyCanSee(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	roomID := makeRoom(t, handler, admin, "deploys", "public")
	app := registerApp(t, handler, admin, "channel.read", "channel.post")
	approveApp(t, handler, admin, app["id"].(string), []string{"channel.post", "channel.read"}, roomID)

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	installed := call(t, handler, http.MethodGet, "/api/applications", nil, member)
	if installed.Code != http.StatusOK || !strings.Contains(installed.Body.String(), "Deploy bot") ||
		!strings.Contains(installed.Body.String(), "channel.read") || !strings.Contains(installed.Body.String(), `"approved"`) {
		t.Errorf("an ordinary member sees %d %s", installed.Code, installed.Body.String())
	}

	here := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/applications", nil, member)
	if here.Code != http.StatusOK || !strings.Contains(here.Body.String(), "Deploy bot") {
		t.Errorf("a member of the channel cannot find the application that reads it: %d %s", here.Code, here.Body.String())
	}

	// But managing them is not theirs.
	if code := call(t, handler, http.MethodGet, "/api/admin/applications", nil, member).Code; code != http.StatusForbidden {
		t.Errorf("an ordinary member reached the administration of applications: %d", code)
	}
}

/*
Removing an application ends its access at once, and what it posted is kept or
taken down as the operator chose.
*/
func TestRemovalEndsAccessAndAppliesTheContentChoice(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	roomID := makeRoom(t, handler, admin, "deploys", "public")

	for _, choice := range []string{"retain", "remove"} {
		app := registerApp(t, handler, admin, "channel.post")
		id := app["id"].(string)
		credential := approveApp(t, handler, admin, id, []string{"channel.post"}, roomID)
		body := "posted, then " + choice
		if code := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
			map[string]any{"body": body}, credential).Code; code != http.StatusCreated {
			t.Fatalf("posting returned %d", code)
		}

		removed := call(t, handler, http.MethodPost, "/api/admin/applications/"+id+"/remove",
			map[string]any{"content": choice}, admin)
		if removed.Code != http.StatusOK {
			t.Fatalf("removing with %q returned %d: %s", choice, removed.Code, removed.Body.String())
		}
		if code := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
			map[string]any{"body": "after removal"}, credential).Code; code != http.StatusUnauthorized {
			t.Errorf("a removed application's credential returned %d, want 401", code)
		}

		present := strings.Contains(call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, admin).Body.String(), body)
		if choice == "retain" && !present {
			t.Error("retained content was taken down")
		}
		if choice == "remove" && present {
			t.Error("content chosen for removal is still there")
		}
	}

	if code := call(t, handler, http.MethodPost, "/api/admin/applications/"+
		registerApp(t, handler, admin)["id"].(string)+"/remove", map[string]any{}, admin).Code; code != http.StatusBadRequest {
		t.Errorf("removing without saying what to do with its content returned %d", code)
	}
}

// Registration, approval and removal are each in the audit log, with who.
func TestApplicationsAreAudited(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	roomID := makeRoom(t, handler, admin, "deploys", "public")
	app := registerApp(t, handler, admin, "channel.post")
	id := app["id"].(string)
	approveApp(t, handler, admin, id, []string{"channel.post"}, roomID)
	call(t, handler, http.MethodPost, "/api/admin/applications/"+id+"/remove", map[string]any{"content": "retain"}, admin)

	var entries []struct {
		Action   string `json:"action"`
		TargetID string `json:"targetId"`
		Actor    string `json:"actor"`
	}
	if err := json.Unmarshal(call(t, handler, http.MethodGet, "/api/admin/audit", nil, admin).Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, entry := range entries {
		if entry.TargetID == id {
			seen[entry.Action] = entry.Actor
		}
	}
	for _, action := range []string{"integration.application.registered", "integration.application.approved",
		"integration.application.removed"} {
		if seen[action] != "founder" {
			t.Errorf("%s recorded with actor %q", action, seen[action])
		}
	}
	if strings.Contains(call(t, handler, http.MethodGet, "/api/admin/audit", nil, admin).Body.String(), "tdc_") {
		t.Error("the audit log holds a credential")
	}
}
