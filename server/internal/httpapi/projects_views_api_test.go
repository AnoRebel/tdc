package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

func viewResultKeys(t *testing.T, handler http.Handler, token, id string) []string {
	t.Helper()
	body := decode(t, mustCall(t, handler, http.MethodGet, "/api/views/"+id+"/results", nil, token, http.StatusOK))
	keys := []string{}
	for _, raw := range body["issues"].([]any) {
		keys = append(keys, raw.(map[string]any)["key"].(string))
	}
	slices.Sort(keys)
	return keys
}

/*
A view shared across projects shows each person who opens it what they may
see, and no more: it is a question kept, not an answer. A view nobody shared,
or one of a project somebody cannot see, does not exist to them.
*/
func TestASharedViewShowsEachViewerOnlyWhatTheyMaySee(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")

	startProject(t, handler, founder, "OPEN", "public")
	startProject(t, handler, founder, "SHUT", "private")
	for _, key := range []string{"OPEN", "SHUT"} {
		label := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/"+key+"/labels",
			map[string]any{"name": "bug"}, founder, http.StatusCreated))["id"].(string)
		fileIssue(t, handler, founder, key, "Crash in "+key)
		fileIssue(t, handler, founder, key, "Unlabelled in "+key)
		mustCall(t, handler, http.MethodPut, "/api/issues/"+key+"-1/labels/"+label, nil, founder, http.StatusOK)
	}

	shared := decode(t, mustCall(t, handler, http.MethodPost, "/api/views", map[string]any{
		"name": "Every bug", "filter": map[string]any{"label": "bug"}, "shared": true,
	}, founder, http.StatusCreated))["id"].(string)

	if keys := viewResultKeys(t, handler, founder, shared); !slices.Equal(keys, []string{"OPEN-1", "SHUT-1"}) {
		t.Errorf("the owner's view shows %v", keys)
	}
	if keys := viewResultKeys(t, handler, amina, shared); !slices.Equal(keys, []string{"OPEN-1"}) {
		t.Errorf("a viewer outside the private project sees %v", keys)
	}
	found := false
	for _, view := range decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/views", nil, amina, http.StatusOK).Body.Bytes()) {
		if view["id"] == shared {
			found = true
			if view["mine"] != false {
				t.Error("a view somebody else saved is reported as the viewer's")
			}
		}
	}
	if !found {
		t.Error("a shared view across projects is not listed for others")
	}

	// Unshared: the owner's own.
	private := decode(t, mustCall(t, handler, http.MethodPost, "/api/views", map[string]any{
		"name": "Mine", "filter": map[string]any{"q": "crash"},
	}, founder, http.StatusCreated))["id"].(string)
	for _, path := range []string{"/api/views/" + private, "/api/views/" + private + "/results"} {
		mustCall(t, handler, http.MethodGet, path, nil, amina, http.StatusNotFound)
	}

	// Shared, but in a project amina cannot see.
	hidden := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/SHUT/views", map[string]any{
		"name": "Team board", "layout": "board", "shared": true,
	}, founder, http.StatusCreated))["id"].(string)
	mustCall(t, handler, http.MethodGet, "/api/views/"+hidden+"/results", nil, amina, http.StatusNotFound)

	// Only its owner changes a view, and only members share one of a project.
	mustCall(t, handler, http.MethodPatch, "/api/views/"+shared, map[string]any{
		"name": "Taken", "shared": false,
	}, amina, http.StatusForbidden)
	mustCall(t, handler, http.MethodDelete, "/api/views/"+shared, nil, amina, http.StatusForbidden)
	mustCall(t, handler, http.MethodPost, "/api/projects/OPEN/views", map[string]any{
		"name": "For everyone", "shared": true,
	}, amina, http.StatusForbidden)
	mustCall(t, handler, http.MethodPost, "/api/projects/OPEN/views", map[string]any{
		"name": "For me",
	}, amina, http.StatusCreated)
	mustCall(t, handler, http.MethodPost, "/api/views", map[string]any{
		"name": "A board of everything", "layout": "board",
	}, founder, http.StatusBadRequest)
}

/*
A batch reports on every issue: what changed, what was already so, what the
actor may not touch and why, and what is not there. Each change made is in its
own issue's history, as if made alone.
*/
func TestABulkChangeReportsEveryIssue(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")

	startProject(t, handler, founder, "TEAM", "public")
	startProject(t, handler, founder, "ELSE", "public")
	label := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/TEAM/labels",
		map[string]any{"name": "triage"}, founder, http.StatusCreated))["id"].(string)
	fileIssue(t, handler, founder, "TEAM", "The founder's")
	fileIssue(t, handler, founder, "ELSE", "Elsewhere")

	// Amina files two while a member, then is not one: she may still label
	// what she wrote, and nothing else.
	mustCall(t, handler, http.MethodPut, "/api/projects/TEAM/members/amina",
		map[string]any{"role": "member"}, founder, http.StatusNoContent)
	fileIssue(t, handler, amina, "TEAM", "Amina's first")
	fileIssue(t, handler, amina, "TEAM", "Amina's second")
	mustCall(t, handler, http.MethodDelete, "/api/projects/TEAM/members/amina", nil, founder, http.StatusNoContent)

	body := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/TEAM/issues/bulk", map[string]any{
		"issues": []string{"TEAM-2", "TEAM-1", "team-3", "ELSE-1", "TEAM-99"},
		"change": map[string]any{"kind": "label", "value": label},
	}, amina, http.StatusOK))
	results := body["results"].([]any)
	want := []struct{ issue, code, permission string }{
		{"TEAM-2", "changed", ""},
		{"TEAM-1", "refused", "issue.label"},
		{"TEAM-3", "changed", ""},
		{"ELSE-1", "not_found", ""},
		{"TEAM-99", "not_found", ""},
	}
	if len(results) != len(want) {
		t.Fatalf("a batch of %d came back with %d results: %v", len(want), len(results), results)
	}
	for i, expected := range want {
		result := results[i].(map[string]any)
		permission, _ := result["permission"].(string)
		if result["issue"] != expected.issue || result["code"] != expected.code || permission != expected.permission {
			t.Errorf("result %d is %v, want %+v", i, result, expected)
		}
		if result["code"] != "changed" && result["reason"] == "" {
			t.Errorf("result %d says no reason: %v", i, result)
		}
	}

	for _, ref := range []string{"TEAM-2", "TEAM-3"} {
		if types := historyTypes(t, handler, founder, ref); types != "issue.created,issue.labeled" {
			t.Errorf("%s's history reads %s", ref, types)
		}
	}
	if types := historyTypes(t, handler, founder, "TEAM-1"); types != "issue.created" {
		t.Errorf("a refused issue's history reads %s", types)
	}

	// Asking again changes nothing and says so.
	again := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/TEAM/issues/bulk", map[string]any{
		"issues": []string{"TEAM-2"}, "change": map[string]any{"kind": "label", "value": label},
	}, amina, http.StatusOK))["results"].([]any)
	if again[0].(map[string]any)["code"] != "unchanged" {
		t.Errorf("labelling twice: %v", again)
	}

	// A change that cannot mean anything is refused whole, before anything.
	mustCall(t, handler, http.MethodPost, "/api/projects/TEAM/issues/bulk", map[string]any{
		"issues": []string{"TEAM-1"}, "change": map[string]any{"kind": "explode"},
	}, founder, http.StatusBadRequest)
	mustCall(t, handler, http.MethodPost, "/api/projects/TEAM/issues/bulk", map[string]any{
		"issues": []string{}, "change": map[string]any{"kind": "label", "value": label},
	}, founder, http.StatusBadRequest)
}
