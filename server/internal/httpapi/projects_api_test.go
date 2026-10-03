package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// registerMember makes an ordinary account and returns its token.
func registerMember(t *testing.T, handler http.Handler, username string) string {
	t.Helper()
	mustCall(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": username, "password": testPassword,
	}, "", http.StatusCreated)
	return signIn(t, handler, username, testPassword)
}

// startProject makes a project and returns it as the server described it.
func startProject(t *testing.T, handler http.Handler, token, key, visibility string) map[string]any {
	t.Helper()
	return decode(t, mustCall(t, handler, http.MethodPost, "/api/projects", map[string]any{
		"key": key, "name": "Project " + key, "visibility": visibility,
	}, token, http.StatusCreated))
}

// fileIssue files an issue and returns it.
func fileIssue(t *testing.T, handler http.Handler, token, key, title string) map[string]any {
	t.Helper()
	return decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/"+key+"/issues", map[string]any{
		"title": title,
	}, token, http.StatusCreated))
}

// stateNamed finds one of a project's states by name.
func stateNamed(t *testing.T, project map[string]any, name string) string {
	t.Helper()
	states, _ := project["states"].([]any)
	for _, raw := range states {
		state := raw.(map[string]any)
		if state["name"] == name {
			return state["id"].(string)
		}
	}
	t.Fatalf("project has no state %q: %v", name, states)
	return ""
}

func decodeRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var list []map[string]any
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode list %q: %v", body, err)
	}
	return list
}

// A project's key is in every identifier made under it, the key is upper-cased
// rather than refused, and a second project cannot take it — in any case.
func TestAProjectKeyNamesItsIssuesAndCannotBeTaken(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)

	project := startProject(t, handler, founder, "web", "public")
	if project["key"] != "WEB" {
		t.Fatalf("key = %v, want WEB", project["key"])
	}

	for i := 1; i <= 3; i++ {
		issue := fileIssue(t, handler, founder, "WEB", fmt.Sprintf("Issue %d", i))
		if want := fmt.Sprintf("WEB-%d", i); issue["key"] != want {
			t.Errorf("issue %d is %v, want %s", i, issue["key"], want)
		}
	}

	for _, taken := range []string{"WEB", "web", "Web"} {
		recorder := call(t, handler, http.MethodPost, "/api/projects", map[string]any{
			"key": taken, "name": "Again",
		}, founder)
		if recorder.Code != http.StatusConflict {
			t.Errorf("a second project keyed %q: status %d, want 409", taken, recorder.Code)
		}
	}

	for _, malformed := range []string{"W", "1WEB", "WEB-1", "TOOLONGAKEY", "wé"} {
		recorder := call(t, handler, http.MethodPost, "/api/projects", map[string]any{
			"key": malformed, "name": "Bad",
		}, founder)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("key %q: status %d, want 400", malformed, recorder.Code)
		}
	}
}

// The identifier is the one thing about an issue that never changes: not when
// it is renamed, not when it moves, not when its project is renamed.
func TestAnIssueKeepsItsIdentifier(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	project := startProject(t, handler, founder, "API", "public")
	issue := fileIssue(t, handler, founder, "API", "Rate limit the search endpoint")

	mustCall(t, handler, http.MethodPatch, "/api/issues/API-1", map[string]any{
		"title": "Rate limit every search endpoint",
	}, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/issues/API-1/state", map[string]any{
		"stateId": stateNamed(t, project, "In progress"),
	}, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPatch, "/api/projects/API", map[string]any{
		"name": "The public API",
	}, founder, http.StatusOK)

	after := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/api-1", nil, founder, http.StatusOK))
	if after["key"] != "API-1" || after["id"] != issue["id"] {
		t.Fatalf("after renaming and moving, the issue is %v (%v), want API-1 (%v)",
			after["key"], after["id"], issue["id"])
	}
	if after["title"] != "Rate limit every search endpoint" {
		t.Errorf("title = %v", after["title"])
	}
}

/*
A private project is absent, not refused.

Every way of reaching it — the list, the project by key, an issue by its
identifier, its issue list, its members, filing into it — answers a
non-member exactly as it answers a project that does not exist.
*/
func TestAPrivateProjectDoesNotExistToAnOutsider(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	outsider := registerMember(t, handler, "outsider")

	startProject(t, handler, founder, "SEC", "private")
	fileIssue(t, handler, founder, "SEC", "A vulnerability report")

	list := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/projects", nil, outsider, http.StatusOK).Body.Bytes())
	for _, project := range list {
		if project["key"] == "SEC" {
			t.Fatal("a private project appears in an outsider's list")
		}
	}

	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/projects/SEC"},
		{http.MethodGet, "/api/projects/SEC/issues"},
		{http.MethodGet, "/api/projects/SEC/members"},
		{http.MethodPost, "/api/projects/SEC/issues"},
		{http.MethodGet, "/api/issues/SEC-1"},
		{http.MethodGet, "/api/issues/SEC-1/history"},
	} {
		private := call(t, handler, probe.method, probe.path, map[string]any{"title": "x"}, outsider)
		absent := call(t, handler, probe.method,
			strings.ReplaceAll(probe.path, "SEC", "NOPE"), map[string]any{"title": "x"}, outsider)
		if private.Code != http.StatusNotFound {
			t.Errorf("%s %s: status %d, want 404", probe.method, probe.path, private.Code)
		}
		if private.Code != absent.Code || private.Body.String() != absent.Body.String() {
			t.Errorf("%s %s answers differently from a project that does not exist:\n  %s\n  %s",
				probe.method, probe.path, private.Body.String(), absent.Body.String())
		}
	}

	// And the discussion room behind the issue is no way in either.
	issue := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/SEC-1", nil, founder, http.StatusOK))
	room := call(t, handler, http.MethodGet, "/api/rooms/"+issue["id"].(string)+"/timeline", nil, outsider)
	if room.Code != http.StatusNotFound {
		t.Errorf("the discussion's timeline answered an outsider with %d, want 404", room.Code)
	}
}

// Filing into a public project one is not in, and changing its states, name
// the permission that would have allowed it.
func TestARefusalNamesThePermission(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	visitor := registerMember(t, handler, "visitor")
	startProject(t, handler, founder, "OPEN", "public")

	recorder := call(t, handler, http.MethodPost, "/api/projects/OPEN/states",
		map[string]any{"name": "Review"}, visitor)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "project.configure_states") {
		t.Errorf("adding a state as a visitor: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = call(t, handler, http.MethodPatch, "/api/projects/OPEN",
		map[string]any{"visibility": "private"}, visitor)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "project.administer") {
		t.Errorf("closing a project as a visitor: %d %s", recorder.Code, recorder.Body.String())
	}

	// Once in it, they may file.
	mustCall(t, handler, http.MethodPut, "/api/projects/OPEN/members/visitor",
		map[string]any{"role": "member"}, founder, http.StatusNoContent)
	fileIssue(t, handler, visitor, "OPEN", "Now I may")
}

// A state that does not exist, or belongs to another project, is not somewhere
// an issue can go — and which states mean "done" is stated, not guessed.
func TestAnIssueMovesOnlyToItsProjectsStates(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	project := startProject(t, handler, founder, "ONE", "public")
	other := startProject(t, handler, founder, "TWO", "public")
	fileIssue(t, handler, founder, "ONE", "Stay in your lane")

	for _, target := range []string{
		"00000000-0000-0000-0000-000000000000",
		stateNamed(t, other, "Done"),
		"not-a-state",
	} {
		recorder := call(t, handler, http.MethodPost, "/api/issues/ONE-1/state",
			map[string]any{"stateId": target}, founder)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("moving to %s: status %d, want 400", target, recorder.Code)
		}
	}

	issue := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/ONE-1", nil, founder, http.StatusOK))
	if issue["state"].(map[string]any)["id"] != stateNamed(t, project, "To do") {
		t.Errorf("a refused move moved the issue: %v", issue["state"])
	}

	completions := 0
	for _, raw := range project["states"].([]any) {
		if raw.(map[string]any)["isCompletion"] == true {
			completions++
		}
	}
	if completions != 1 {
		t.Errorf("a new project has %d completion states, want exactly one", completions)
	}
}

// A state issues are in is removed only by saying where they go; then each is
// moved, recorded, and nothing is left naming the removed state.
func TestRemovingAStateInUse(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	project := startProject(t, handler, founder, "FLOW", "public")
	todo := stateNamed(t, project, "To do")
	doing := stateNamed(t, project, "In progress")

	fileIssue(t, handler, founder, "FLOW", "First")
	fileIssue(t, handler, founder, "FLOW", "Second")

	recorder := call(t, handler, http.MethodDelete, "/api/projects/FLOW/states/"+todo, nil, founder)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("removing a state in use without a destination: %d, want 409", recorder.Code)
	}

	mustCall(t, handler, http.MethodDelete, "/api/projects/FLOW/states/"+todo+"?moveTo="+doing,
		nil, founder, http.StatusNoContent)

	for _, ref := range []string{"FLOW-1", "FLOW-2"} {
		issue := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/"+ref, nil, founder, http.StatusOK))
		if got := issue["state"].(map[string]any)["id"]; got != doing {
			t.Errorf("%s is in %v, want the destination %s", ref, got, doing)
		}

		history := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/issues/"+ref+"/history",
			nil, founder, http.StatusOK).Body.Bytes())
		last := history[len(history)-1]
		if last["type"] != "issue.state_changed" {
			t.Errorf("%s's move was not recorded in its history: %v", ref, last)
		}
	}

	after := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/FLOW", nil, founder, http.StatusOK))
	if len(after["states"].([]any)) != 2 {
		t.Errorf("the project has %d states after removing one of three", len(after["states"].([]any)))
	}

	// The last state cannot go at all.
	remaining := after["states"].([]any)
	mustCall(t, handler, http.MethodDelete, "/api/projects/FLOW/states/"+
		remaining[1].(map[string]any)["id"].(string)+"?moveTo="+doing, nil, founder, http.StatusNoContent)
	mustCall(t, handler, http.MethodDelete, "/api/projects/FLOW/states/"+doing, nil, founder, http.StatusConflict)
}

// An issue's history is every change in order, with who made it and what it
// was before — and it can be read, never written.
func TestAnIssuesHistoryIsOrderedAndReadOnly(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	project := startProject(t, handler, founder, "HIST", "public")
	fileIssue(t, handler, founder, "HIST", "Record me")

	for _, name := range []string{"In progress", "Done", "To do"} {
		mustCall(t, handler, http.MethodPost, "/api/issues/HIST-1/state", map[string]any{
			"stateId": stateNamed(t, project, name),
		}, founder, http.StatusOK)
	}

	history := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/issues/HIST-1/history",
		nil, founder, http.StatusOK).Body.Bytes())
	wantTypes := []string{"issue.created", "issue.state_changed", "issue.state_changed", "issue.state_changed"}
	wantNames := []string{"", "In progress", "Done", "To do"}
	if len(history) != len(wantTypes) {
		t.Fatalf("history has %d entries, want %d: %v", len(history), len(wantTypes), history)
	}
	previousSequence := 0.0
	for i, entry := range history {
		if entry["type"] != wantTypes[i] {
			t.Errorf("entry %d is %v, want %s", i, entry["type"], wantTypes[i])
		}
		if sequence := entry["sequence"].(float64); sequence <= previousSequence {
			t.Errorf("entry %d is out of order: %v after %v", i, sequence, previousSequence)
		} else {
			previousSequence = sequence
		}
		if entry["actor"] != "founder@tdc.test" {
			t.Errorf("entry %d names %v as its actor", i, entry["actor"])
		}
		if wantNames[i] != "" {
			content := entry["content"].(map[string]any)
			if content["newName"] != wantNames[i] || content["field"] != "state" || content["previous"] == nil {
				t.Errorf("entry %d does not say what changed: %v", i, content)
			}
		}
	}

	// There is nothing that writes it.
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodPost} {
		recorder := call(t, handler, method, "/api/issues/HIST-1/history", map[string]any{}, founder)
		if recorder.Code != http.StatusMethodNotAllowed && recorder.Code != http.StatusNotFound {
			t.Errorf("%s on the history answered %d", method, recorder.Code)
		}
	}

	// Nor can it be reached as a room: the stream has no members, so the
	// room's own edit and delete paths refuse even the server's founder.
	stream := project["id"].(string)
	eventID := history[1]["id"].(string)
	for _, attempt := range []struct{ method, path string }{
		{http.MethodDelete, "/api/rooms/" + stream + "/messages/" + eventID},
		{http.MethodPatch, "/api/rooms/" + stream + "/messages/" + eventID},
		{http.MethodGet, "/api/rooms/" + stream + "/timeline"},
	} {
		recorder := call(t, handler, attempt.method, attempt.path, map[string]any{"body": "rewritten"}, founder)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s answered %d, want 404", attempt.method, attempt.path, recorder.Code)
		}
	}

	again := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/issues/HIST-1/history",
		nil, founder, http.StatusOK).Body.Bytes())
	if len(again) != len(history) {
		t.Errorf("the history changed from %d to %d entries", len(history), len(again))
	}
}

// Neither a project's stream nor an issue's discussion appears among the
// conversations a person is shown, nor counts against the channel limit.
func TestProjectRoomsAreNotConversations(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	startProject(t, handler, founder, "HIDE", "public")
	fileIssue(t, handler, founder, "HIDE", "Not a channel")

	rooms := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/rooms", nil, founder, http.StatusOK).Body.Bytes())
	for _, room := range rooms {
		if room["kind"] == "project" || room["kind"] == "issue-discussion" {
			t.Errorf("a %v room is listed among conversations: %v", room["kind"], room["name"])
		}
	}

	// A server allowing one channel still allows it: the project's stream and
	// the issue's discussion are rooms, but nobody added them as channels.
	mustCall(t, handler, http.MethodPut, "/api/admin/settings/limits.max_channels",
		map[string]any{"value": "1"}, founder, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, "/api/rooms", map[string]any{"slug": "the-one"},
		founder, http.StatusCreated)
}
