package httpapi_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

/*
The event catalogue past one channel: projects and issues, the whole server,
and the events that belong to no room — each delivered with its documented
payload, and never to a subscriber who could not read what it describes.
*/

// subscribeTo makes a subscription from a request body as given, expecting it
// to be made.
func subscribeTo(t *testing.T, handler http.Handler, token string, body map[string]any) map[string]any {
	t.Helper()
	body["label"] = "events"
	return decode(t, mustCall(t, handler, http.MethodPost, "/api/me/webhooks", body, token, http.StatusCreated))
}

// payloads decodes everything a receiver was sent.
func payloads(t *testing.T, endpoint *receiver) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, got := range endpoint.deliveries() {
		var payload map[string]any
		if err := json.Unmarshal(got.body, &payload); err != nil {
			t.Fatalf("a delivery was not JSON: %s", got.body)
		}
		out = append(out, payload)
	}
	return out
}

// ofType finds the first payload of a type, failing if there is none.
func ofType(t *testing.T, endpoint *receiver, kind string) map[string]any {
	t.Helper()
	for _, payload := range payloads(t, endpoint) {
		if payload["type"] == kind {
			return payload
		}
	}
	t.Fatalf("no %s was delivered; got %v", kind, typesOf(t, endpoint))
	return nil
}

func typesOf(t *testing.T, endpoint *receiver) []string {
	t.Helper()
	var kinds []string
	for _, payload := range payloads(t, endpoint) {
		kinds = append(kinds, payload["type"].(string))
	}
	return kinds
}

func wasDelivered(t *testing.T, endpoint *receiver, kind string) bool {
	t.Helper()
	return slices.Contains(typesOf(t, endpoint), kind)
}

/*
A project's events reach a subscription on the project with the project and
issue named, a closing and a reopening go by their own names as well as as a
move, and a comment in an issue's discussion is delivered as issue.commented.

Somebody outside the private project can neither subscribe to it nor hear of
it through a subscription to the whole server.
*/
func TestAProjectsEventsReachOnlyThoseWhoMayReadIt(t *testing.T) {
	handler, founder, _ := webhookFixture(t, nil)
	project := startProject(t, handler, founder, "PRIV", "private")
	projectID := project["id"].(string)
	outsider := registerMember(t, handler, "outsider")

	inside := newReceiver(t, answering(http.StatusNoContent))
	subscribeTo(t, handler, founder, map[string]any{
		"scope": "project", "projectId": projectID, "url": inside.URL,
		"eventTypes": []string{"issue.created", "issue.state_changed", "issue.closed",
			"issue.reopened", "issue.commented", "issue.title_changed"},
	})

	// The outsider cannot subscribe to the project, and is told it is missing.
	recorder := call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"scope": "project", "projectId": projectID, "label": "spy", "url": "https://example.org/hook",
		"eventTypes": []string{"issue.created"},
	}, outsider)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("subscribing to a private project from outside returned %d: %s", recorder.Code, recorder.Body.String())
	}
	// And a subscription to the whole server hears nothing of it.
	outside := newReceiver(t, answering(http.StatusNoContent))
	subscribeTo(t, handler, outsider, map[string]any{
		"scope": "server", "url": outside.URL,
		"eventTypes": []string{"issue.created", "issue.closed", "issue.commented", "message.sent"},
	})

	issue := fileIssue(t, handler, founder, "PRIV", "The build is slow")
	mustCall(t, handler, http.MethodPost, "/api/issues/PRIV-1/state",
		map[string]any{"stateId": stateNamed(t, project, "Done")}, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/issues/PRIV-1/state",
		map[string]any{"stateId": stateNamed(t, project, "To do")}, founder, http.StatusOK)
	comment(t, handler, founder, issue["id"].(string), "Profiling shows the linker")

	eventually(t, 10*time.Second, "the project's events", func() bool {
		return len(inside.deliveries()) >= 6
	})

	created := ofType(t, inside, "issue.created")
	if ref, _ := created["project"].(map[string]any); ref["key"] != "PRIV" || ref["id"] != projectID {
		t.Errorf("issue.created named the project %v", created["project"])
	}
	if room, _ := created["room"].(map[string]any); room["id"] != projectID {
		t.Errorf("issue.created named the room %v, want the project's stream", created["room"])
	}
	if content := created["event"].(map[string]any)["content"].(map[string]any); content["key"] != "PRIV-1" {
		t.Errorf("issue.created carried %v", content)
	}

	closed := ofType(t, inside, "issue.closed")
	if event := closed["event"].(map[string]any); event["type"] != "issue.state_changed" {
		t.Errorf("issue.closed carried a %v event, want the move itself", event["type"])
	}
	ofType(t, inside, "issue.reopened")
	if moves := strings.Count(strings.Join(typesOf(t, inside), " "), "issue.state_changed"); moves != 2 {
		t.Errorf("issue.state_changed was delivered %d times, want both moves", moves)
	}

	commented := ofType(t, inside, "issue.commented")
	if ref, _ := commented["issue"].(map[string]any); ref["key"] != "PRIV-1" || ref["id"] != issue["id"] {
		t.Errorf("issue.commented named the issue %v", commented["issue"])
	}
	if body := commented["event"].(map[string]any)["content"].(map[string]any)["body"]; body != "Profiling shows the linker" {
		t.Errorf("issue.commented carried %v", body)
	}
	if wasDelivered(t, inside, "issue.title_changed") {
		t.Error("a type nobody caused was delivered")
	}

	// Given time to arrive, nothing about the private project reached outside.
	time.Sleep(500 * time.Millisecond)
	if got := typesOf(t, outside); len(got) != 0 {
		t.Errorf("somebody outside a private project was told %v", got)
	}
}

/*
A project subscription asks for a project's events and nothing else, and a
channel subscription a channel's: a type that cannot happen there is refused,
and so is a channel named as a project.
*/
func TestASubscriptionsTypesMustFitWhatItListensTo(t *testing.T) {
	handler, founder, roomID := webhookFixture(t, nil)
	project := startProject(t, handler, founder, "FIT", "public")

	for name, body := range map[string]map[string]any{
		"an issue type on a channel": {"roomId": roomID, "eventTypes": []string{"issue.created"}},
		"a message type on a project": {
			"scope": "project", "projectId": project["id"], "eventTypes": []string{"message.sent"},
		},
		"a channel named as a project": {
			"scope": "project", "projectId": roomID, "eventTypes": []string{"issue.created"},
		},
		"a project named as a channel": {"roomId": project["id"], "eventTypes": []string{"message.sent"}},
		"a new account on a channel":   {"roomId": roomID, "eventTypes": []string{"account.created"}},
	} {
		body["label"] = "x"
		body["url"] = "https://example.org/hook"
		if code := call(t, handler, http.MethodPost, "/api/me/webhooks", body, founder).Code; code != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", name, code)
		}
	}
}

/*
A server subscription hears the rooms its owner can read and no others: a
private channel made by somebody else is not announced to them, a public one
is, and so is a join anywhere they can read.
*/
func TestAServerSubscriptionHearsOnlyWhatItsOwnerCanRead(t *testing.T) {
	handler, admin, _ := webhookFixture(t, nil)
	amina := registerMember(t, handler, "amina")

	endpoint := newReceiver(t, answering(http.StatusNoContent))
	subscribeTo(t, handler, amina, map[string]any{
		"scope": "server", "url": endpoint.URL,
		"eventTypes": []string{"room.created", "member.joined", "message.sent"},
	})

	secret := makeRoom(t, handler, admin, "board", "private")
	postMessage(t, handler, admin, secret, "Salaries for next year")
	open := makeRoom(t, handler, admin, "lobby", "public")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+open+"/join", nil, amina, http.StatusOK)

	eventually(t, 10*time.Second, "the public channel's events", func() bool {
		return wasDelivered(t, endpoint, "room.created") && wasDelivered(t, endpoint, "member.joined")
	})
	time.Sleep(300 * time.Millisecond)
	for _, payload := range payloads(t, endpoint) {
		if room, _ := payload["room"].(map[string]any); room["id"] == secret {
			t.Errorf("a private channel's %v reached somebody outside it", payload["type"])
		}
	}
	created := ofType(t, endpoint, "room.created")
	if room, _ := created["room"].(map[string]any); room["id"] != open {
		t.Errorf("room.created named %v, want the public channel", created["room"])
	}
}

/*
Joining, leaving and a call starting and ending are delivered to a channel's
subscribers. Joins and leaves were offered before and never sent, because the
paths that store them publish nothing live; they are queued where every event
is stored now.
*/
func TestJoinsLeavesAndCallsAreDelivered(t *testing.T) {
	handler, admin, roomID := webhookFixture(t, nil)
	setFeature(t, handler, admin, "calls", true)
	amina := registerMember(t, handler, "amina")

	endpoint := newReceiver(t, answering(http.StatusNoContent))
	subscribeTo(t, handler, admin, map[string]any{
		"roomId": roomID, "url": endpoint.URL,
		"eventTypes": []string{"member.joined", "member.left", "call.started", "call.ended"},
	})

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, amina, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/call",
		map[string]any{"audio": true}, amina, http.StatusOK)
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+roomID+"/call", nil, amina, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/leave", nil, amina, http.StatusNoContent)

	eventually(t, 10*time.Second, "the join, the call and the leave", func() bool {
		return len(endpoint.deliveries()) >= 4
	})
	joined := ofType(t, endpoint, "member.joined")
	if member := joined["event"].(map[string]any)["content"].(map[string]any)["member"]; !strings.HasPrefix(member.(string), "amina@") {
		t.Errorf("member.joined named %v", member)
	}
	for _, kind := range []string{"member.left", "call.started", "call.ended"} {
		ofType(t, endpoint, kind)
	}
}

/*
A new account and a change of role belong to no room. They reach a server
subscription whose owner may read every account — without anybody's email
address — and nobody else may ask for them. Somebody who could ask, and has
lost the permission since, is told nothing more.
*/
func TestPeopleEventsNeedThePermissionToReadEveryAccount(t *testing.T) {
	handler, admin, _ := webhookFixture(t, nil)
	member := registerMember(t, handler, "member")

	recorder := call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"scope": "server", "label": "x", "url": "https://example.org/hook",
		"eventTypes": []string{"account.created"},
	}, member)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("asking for new accounts without user.read.all returned %d: %s", recorder.Code, recorder.Body.String())
	}

	endpoint := newReceiver(t, answering(http.StatusNoContent))
	subscribeTo(t, handler, admin, map[string]any{
		"scope": "server", "url": endpoint.URL,
		"eventTypes": []string{"account.created", "account.role_changed"},
	})

	// A member granted the permission by a role may ask too — until the role
	// is taken away.
	role := decode(t, mustCall(t, handler, http.MethodPost, "/api/admin/roles",
		map[string]string{"name": "Registrar"}, admin, http.StatusCreated))
	roleID := role["id"].(string)
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+roleID+"/permissions",
		map[string]any{"permission": "user.read.all", "granted": true}, admin, http.StatusNoContent)
	memberID := accountID(t, handler, member)
	mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+roleID+"/members",
		map[string]string{"user": memberID}, admin, http.StatusNoContent)
	registrar := newReceiver(t, answering(http.StatusNoContent))
	subscribeTo(t, handler, member, map[string]any{
		"scope": "server", "url": registrar.URL, "eventTypes": []string{"account.created"},
	})
	mustCall(t, handler, http.MethodDelete, "/api/admin/roles/"+roleID+"/members/"+memberID,
		nil, admin, http.StatusNoContent)

	mustCall(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "newcomer", "password": testPassword, "email": "newcomer@example.org",
	}, "", http.StatusCreated)

	eventually(t, 10*time.Second, "the new account and the role changes", func() bool {
		return wasDelivered(t, endpoint, "account.created") &&
			strings.Count(strings.Join(typesOf(t, endpoint), " "), "account.role_changed") == 2
	})
	created := ofType(t, endpoint, "account.created")
	if _, hasRoom := created["room"]; hasRoom {
		t.Errorf("an event in no room named one: %v", created["room"])
	}
	account := created["event"].(map[string]any)["content"].(map[string]any)["account"].(map[string]any)
	if account["username"] != "newcomer" || !strings.HasPrefix(account["address"].(string), "newcomer@") {
		t.Errorf("account.created described %v", account)
	}
	raw, _ := json.Marshal(payloads(t, endpoint))
	if strings.Contains(string(raw), "newcomer@example.org") {
		t.Error("a delivery carried somebody's email address")
	}
	changed := ofType(t, endpoint, "account.role_changed")
	if content := changed["event"].(map[string]any)["content"].(map[string]any); content["change"] != "assigned" ||
		content["role"].(map[string]any)["name"] != "Registrar" {
		t.Errorf("account.role_changed carried %v", content)
	}

	time.Sleep(300 * time.Millisecond)
	if got := typesOf(t, registrar); len(got) != 0 {
		t.Errorf("somebody who lost user.read.all was still told %v", got)
	}
}

/*
An application subscribes through the same endpoint, held to its grant: a
channel it was not given is refused even when public, a server subscription
hears only its own channels, and a permission it was not granted cannot be
asked for.
*/
func TestAnApplicationsSubscriptionsAreHeldToItsGrant(t *testing.T) {
	handler, admin, roomID, _, credential := botFixture(t, "channel.read", "channel.post")
	elsewhere := makeRoom(t, handler, admin, "elsewhere", "public")

	recorder := call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"roomId": elsewhere, "label": "x", "url": "https://example.org/hook",
		"eventTypes": []string{"message.sent"},
	}, credential)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("an application subscribing to a channel it was not given returned %d", recorder.Code)
	}
	recorder = call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"scope": "server", "label": "x", "url": "https://example.org/hook",
		"eventTypes": []string{"account.created"},
	}, credential)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("an application asking for new accounts without the grant returned %d", recorder.Code)
	}

	endpoint := newReceiver(t, answering(http.StatusNoContent))
	subscribeTo(t, handler, credential, map[string]any{
		"scope": "server", "url": endpoint.URL, "eventTypes": []string{"message.sent"},
	})
	postMessage(t, handler, admin, elsewhere, "Not for the bot")
	postMessage(t, handler, admin, roomID, "For the bot")

	eventually(t, 10*time.Second, "the message in its channel", func() bool {
		return len(endpoint.deliveries()) >= 1
	})
	time.Sleep(300 * time.Millisecond)
	for _, payload := range payloads(t, endpoint) {
		if body := payload["event"].(map[string]any)["content"].(map[string]any)["body"]; body != "For the bot" {
			t.Errorf("an application was told about %q, outside its channels", body)
		}
	}
}

/*
The catalogue says, for each type, its group, where it can be asked for and
whether the caller could ask for it now.
*/
func TestTheCatalogueDescribesEachType(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	member := registerMember(t, handler, "member")

	read := func(token string) map[string]map[string]any {
		body := decode(t, mustCall(t, handler, http.MethodGet, "/api/webhooks/event-types", nil, token, http.StatusOK))
		byName := map[string]map[string]any{}
		for _, raw := range body["catalogue"].([]any) {
			entry := raw.(map[string]any)
			byName[entry["name"].(string)] = entry
		}
		if len(byName) != len(body["eventTypes"].([]any)) {
			t.Errorf("the catalogue has %d types and the list %d", len(byName), len(body["eventTypes"].([]any)))
		}
		return byName
	}

	asAdmin, asMember := read(admin), read(member)
	if entry := asAdmin["issue.commented"]; entry["group"] != "issues" ||
		!slices.Equal(toStrings(entry["scopes"]), []string{"project", "server"}) {
		t.Errorf("issue.commented is described as %v", entry)
	}
	if asAdmin["account.created"]["available"] != true || asMember["account.created"]["available"] != false {
		t.Errorf("account.created is available to the administrator: %v, to a member: %v",
			asAdmin["account.created"]["available"], asMember["account.created"]["available"])
	}
	if asMember["message.sent"]["available"] != true {
		t.Error("message.sent is not available to a member")
	}
}

func toStrings(raw any) []string {
	var out []string
	for _, value := range raw.([]any) {
		out = append(out, value.(string))
	}
	return out
}
