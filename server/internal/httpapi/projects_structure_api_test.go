package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// keysOf reads the identifiers out of a list of issues, in order.
func keysOf(t *testing.T, recorder []map[string]any) []string {
	t.Helper()
	keys := make([]string, 0, len(recorder))
	for _, issue := range recorder {
		keys = append(keys, issue["key"].(string))
	}
	return keys
}

func listed(t *testing.T, handler http.Handler, token, path string) []string {
	t.Helper()
	return keysOf(t, decodeRows(t, mustCall(t, handler, http.MethodGet, path, nil, token, http.StatusOK).Body.Bytes()))
}

// idNamed finds an entry by name in a list of labels or priorities.
func idNamed(t *testing.T, list any, name string) string {
	t.Helper()
	for _, raw := range list.([]any) {
		entry := raw.(map[string]any)
		if entry["name"] == name {
			return entry["id"].(string)
		}
	}
	t.Fatalf("nothing named %q in %v", name, list)
	return ""
}

func historyTypes(t *testing.T, handler http.Handler, token, ref string) string {
	t.Helper()
	entries := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/issues/"+ref+"/history",
		nil, token, http.StatusOK).Body.Bytes())
	types := make([]string, 0, len(entries))
	for _, entry := range entries {
		types = append(types, entry["type"].(string))
	}
	return strings.Join(types, ",")
}

/*
Assigning somebody tells them, by their own settings, and puts them in the
issue's discussion; taking them off is recorded with who did it. Nobody who
cannot see an issue can be made responsible for it.
*/
func TestAssignmentIsToldAndRecorded(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")
	quiet := registerMember(t, handler, "quiet")
	registerMember(t, handler, "outsider")

	mustCall(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "none"}, quiet, http.StatusNoContent)

	startProject(t, handler, founder, "TEAM", "private")
	for _, person := range []string{"amina", "quiet"} {
		mustCall(t, handler, http.MethodPut, "/api/projects/TEAM/members/"+person,
			map[string]any{"role": "member"}, founder, http.StatusNoContent)
	}
	issue := fileIssue(t, handler, founder, "TEAM", "Ship the release notes")

	for _, person := range []string{"amina", "quiet"} {
		mustCall(t, handler, http.MethodPut, "/api/issues/TEAM-1/assignees/"+person, nil, founder, http.StatusOK)
	}

	assigned := false
	for _, notification := range unreadNotificationsOf(t, handler, amina) {
		if notification["reason"] == "assigned" && notification["roomId"] == issue["id"] {
			assigned = true
			if detail, _ := notification["detail"].(string); !strings.Contains(detail, "TEAM-1") {
				t.Errorf("the assignment does not say which issue: %q", detail)
			}
		}
	}
	if !assigned {
		t.Error("amina was not told she was assigned")
	}
	for _, notification := range unreadNotificationsOf(t, handler, quiet) {
		if notification["reason"] == "assigned" {
			t.Error("somebody who asked to hear nothing was told about an assignment")
		}
	}

	// They are in the discussion, so what is said there reaches them.
	members := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+issue["id"].(string)+"/members",
		nil, founder, http.StatusOK).Body.Bytes())
	inDiscussion := map[string]bool{}
	for _, member := range members {
		inDiscussion[member["username"].(string)] = true
	}
	if !inDiscussion["amina"] || !inDiscussion["quiet"] {
		t.Errorf("assignees are not in the discussion: %v", inDiscussion)
	}

	mustCall(t, handler, http.MethodDelete, "/api/issues/TEAM-1/assignees/quiet", nil, founder, http.StatusOK)
	current := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/TEAM-1", nil, founder, http.StatusOK))
	if assignees := current["assignees"].([]any); len(assignees) != 1 ||
		assignees[0].(map[string]any)["username"] != "amina" {
		t.Errorf("assignees after unassigning are %v", assignees)
	}
	if types := historyTypes(t, handler, founder, "TEAM-1"); types !=
		"issue.created,issue.assigned,issue.assigned,issue.unassigned" {
		t.Errorf("the history reads %s", types)
	}

	// The outsider cannot see a private project's issue, so it cannot be theirs.
	recorder := call(t, handler, http.MethodPut, "/api/issues/TEAM-1/assignees/outsider", nil, founder)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("assigning somebody outside the project: %d %s", recorder.Code, recorder.Body.String())
	}
}

// "My issues" is everything assigned to me in every project I may still see,
// and nothing anybody else holds.
func TestMyIssuesSpanProjectsICanSee(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")
	bola := registerMember(t, handler, "bola")

	startProject(t, handler, founder, "OPEN", "public")
	startProject(t, handler, founder, "SHUT", "private")
	mustCall(t, handler, http.MethodPut, "/api/projects/SHUT/members/amina",
		map[string]any{"role": "member"}, founder, http.StatusNoContent)

	fileIssue(t, handler, founder, "OPEN", "Public work")
	fileIssue(t, handler, founder, "SHUT", "Private work")
	fileIssue(t, handler, founder, "OPEN", "Somebody else's")
	mustCall(t, handler, http.MethodPut, "/api/issues/OPEN-1/assignees/amina", nil, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/issues/SHUT-1/assignees/amina", nil, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/issues/OPEN-2/assignees/bola", nil, founder, http.StatusOK)

	if mine := strings.Join(listed(t, handler, amina, "/api/me/issues"), ","); mine != "SHUT-1,OPEN-1" && mine != "OPEN-1,SHUT-1" {
		t.Errorf("amina's issues are %s", mine)
	}
	if theirs := strings.Join(listed(t, handler, bola, "/api/me/issues"), ","); theirs != "OPEN-2" {
		t.Errorf("bola's issues are %s", theirs)
	}

	// Out of the private project, its issue is no longer hers to see — even
	// though the assignment row is still there.
	mustCall(t, handler, http.MethodDelete, "/api/projects/SHUT/members/amina", nil, founder, http.StatusNoContent)
	if mine := strings.Join(listed(t, handler, amina, "/api/me/issues"), ","); mine != "OPEN-1" {
		t.Errorf("after leaving the private project, amina's issues are %s", mine)
	}
}

/*
Filtering by a label returns only issues carrying it, and sorting by priority
follows the project's own scale — including after that scale is reordered.
*/
func TestLabelsFilterAndPrioritiesOrder(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	project := startProject(t, handler, founder, "SORT", "public")

	bug := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/SORT/labels",
		map[string]any{"name": "bug", "colour": "#dc2626"}, founder, http.StatusCreated))["id"].(string)
	docs := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/SORT/labels",
		map[string]any{"name": "docs"}, founder, http.StatusCreated))["id"].(string)
	mustCall(t, handler, http.MethodPost, "/api/projects/SORT/labels",
		map[string]any{"name": "Bug"}, founder, http.StatusConflict)

	for _, title := range []string{"Crash on save", "Explain the setup", "Wrong example in the docs"} {
		fileIssue(t, handler, founder, "SORT", title)
	}
	mustCall(t, handler, http.MethodPut, "/api/issues/SORT-1/labels/"+bug, nil, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/issues/SORT-2/labels/"+docs, nil, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/issues/SORT-3/labels/"+bug, nil, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/issues/SORT-3/labels/"+docs, nil, founder, http.StatusOK)

	if bugs := strings.Join(listed(t, handler, founder, "/api/projects/SORT/issues?label="+bug), ","); bugs != "SORT-3,SORT-1" {
		t.Errorf("issues labelled bug: %s", bugs)
	}

	scale := project["priorities"]
	urgent, low := idNamed(t, scale, "Urgent"), idNamed(t, scale, "Low")
	mustCall(t, handler, http.MethodPut, "/api/issues/SORT-1/priority", map[string]any{"priorityId": low}, founder, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/issues/SORT-2/priority", map[string]any{"priorityId": urgent}, founder, http.StatusOK)

	if order := strings.Join(listed(t, handler, founder, "/api/projects/SORT/issues?sort=priority"), ","); order != "SORT-2,SORT-1,SORT-3" {
		t.Errorf("by priority: %s, want urgent, then low, then none", order)
	}

	// Reorder the scale so that Low is the most urgent, and the sort follows.
	reordered := []string{low, urgent, idNamed(t, scale, "High"), idNamed(t, scale, "Medium")}
	mustCall(t, handler, http.MethodPut, "/api/projects/SORT/priorities", map[string]any{"order": reordered}, founder, http.StatusOK)
	if order := strings.Join(listed(t, handler, founder, "/api/projects/SORT/issues?sort=priority"), ","); order != "SORT-1,SORT-2,SORT-3" {
		t.Errorf("by the reordered scale: %s", order)
	}

	// Removing a label in use takes it off every issue, each on the record.
	mustCall(t, handler, http.MethodDelete, "/api/projects/SORT/labels/"+docs, nil, founder, http.StatusNoContent)
	if types := historyTypes(t, handler, founder, "SORT-2"); !strings.HasSuffix(types, "issue.unlabeled") {
		t.Errorf("removing the label left no record on the issue: %s", types)
	}
}

/*
Relationships: a parent shows how many of its children are finished, and a
relationship that would make a loop — through any number of issues — is
refused, saying which issues the loop would run through.
*/
func TestRelationshipsReflectChildrenAndRefuseLoops(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	project := startProject(t, handler, founder, "TREE", "public")
	startProject(t, handler, founder, "ELSE", "public")
	fileIssue(t, handler, founder, "ELSE", "Elsewhere")

	for _, title := range []string{"Launch", "Write the copy", "Design the page", "Book the venue"} {
		fileIssue(t, handler, founder, "TREE", title)
	}
	relate := func(ref, kind, other string, want int) string {
		recorder := call(t, handler, http.MethodPost, "/api/issues/"+ref+"/relations",
			map[string]any{"kind": kind, "issue": other}, founder)
		if recorder.Code != want {
			t.Fatalf("%s %s %s: status %d, want %d: %s", ref, kind, other, recorder.Code, want, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	relate("TREE-1", "parent", "TREE-2", http.StatusOK)
	relate("TREE-3", "child", "TREE-1", http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/issues/TREE-2/state", map[string]any{
		"stateId": stateNamed(t, project, "Done"),
	}, founder, http.StatusOK)

	parent := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/TREE-1", nil, founder, http.StatusOK))
	progress := parent["relations"].(map[string]any)["childProgress"].(map[string]any)
	if progress["completed"] != 1.0 || progress["total"] != 2.0 {
		t.Errorf("the parent shows its children as %v, want 1 of 2 done", progress)
	}
	child := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/TREE-2", nil, founder, http.StatusOK))
	if child["relations"].(map[string]any)["parent"].(map[string]any)["key"] != "TREE-1" {
		t.Errorf("the child does not name its parent: %v", child["relations"])
	}

	// A child may not become its own ancestor, however far down.
	relate("TREE-2", "parent", "TREE-4", http.StatusOK)
	refused := relate("TREE-4", "parent", "TREE-1", http.StatusConflict)
	for _, key := range []string{"TREE-1", "TREE-2", "TREE-4"} {
		if !strings.Contains(refused, key) {
			t.Errorf("the refusal does not name %s in the loop: %s", key, refused)
		}
	}
	// Nor have two parents.
	relate("TREE-3", "parent", "TREE-4", http.StatusConflict)

	// Blocking loops, through a chain.
	relate("TREE-1", "blocks", "TREE-3", http.StatusOK)
	relate("TREE-3", "blocks", "TREE-4", http.StatusOK)
	loop := relate("TREE-4", "blocks", "TREE-1", http.StatusConflict)
	if !strings.Contains(loop, "block") {
		t.Errorf("the refusal does not say what kind of loop: %s", loop)
	}
	// And the other way round is the same fact, refused the same way.
	relate("TREE-1", "blocked_by", "TREE-4", http.StatusConflict)

	relate("TREE-1", "blocks", "ELSE-1", http.StatusBadRequest)
	relate("TREE-1", "blocks", "TREE-1", http.StatusBadRequest)

	// Both issues record it, each in its own terms.
	if types := historyTypes(t, handler, founder, "TREE-3"); !strings.Contains(types, "issue.related") {
		t.Errorf("the other side of a relationship has no record of it: %s", types)
	}
}

/*
Somebody allowed to file in a public project without being in it may close and
label the issues they filed, and nobody else's — the per-issue difference that
makes a batch of changes come back mixed.
*/
func TestAnAuthorMayMoveAndLabelTheirOwnIssue(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	visitor := registerMember(t, handler, "visitor")
	project := startProject(t, handler, founder, "OPEN", "public")

	// The operator lets anybody file in public projects.
	memberRole := roleNamed(t, handler, founder, "Member")
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+memberRole+"/permissions",
		map[string]any{"permission": "issue.create", "granted": true}, founder, http.StatusNoContent)

	fileIssue(t, handler, founder, "OPEN", "The founder's")
	fileIssue(t, handler, visitor, "OPEN", "The visitor's")
	label := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/OPEN/labels",
		map[string]any{"name": "question"}, founder, http.StatusCreated))["id"].(string)
	done := stateNamed(t, project, "Done")

	mustCall(t, handler, http.MethodPost, "/api/issues/OPEN-2/state", map[string]any{"stateId": done}, visitor, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/issues/OPEN-2/labels/"+label, nil, visitor, http.StatusOK)

	for _, attempt := range []struct{ method, path, permission string }{
		{http.MethodPost, "/api/issues/OPEN-1/state", "issue.transition"},
		{http.MethodPut, "/api/issues/OPEN-1/labels/" + label, "issue.label"},
		{http.MethodPut, "/api/issues/OPEN-2/assignees/visitor", "issue.assign"},
	} {
		recorder := call(t, handler, attempt.method, attempt.path, map[string]any{"stateId": done}, visitor)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), attempt.permission) {
			t.Errorf("%s %s: %d %s, want a refusal naming %s",
				attempt.method, attempt.path, recorder.Code, recorder.Body.String(), attempt.permission)
		}
	}
}

// A denial written for one project beats even running that project, and
// stops at that project.
func TestADenialInOneProjectBeatsAdministeringIt(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	startProject(t, handler, founder, "KEEP", "public")
	startProject(t, handler, founder, "FREE", "public")
	fileIssue(t, handler, founder, "KEEP", "Keep me")
	fileIssue(t, handler, founder, "FREE", "Free me")

	administrator := roleNamed(t, handler, founder, "Administrator")
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+administrator+"/project-permissions",
		map[string]any{"project": "KEEP", "permission": "issue.delete", "granted": false}, founder, http.StatusNoContent)

	recorder := call(t, handler, http.MethodDelete, "/api/issues/KEEP-1", nil, founder)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "issue.delete") {
		t.Errorf("deleting where it is denied: %d %s", recorder.Code, recorder.Body.String())
	}
	mustCall(t, handler, http.MethodDelete, "/api/issues/FREE-1", nil, founder, http.StatusNoContent)

	// And a channel permission cannot be granted in a project at all.
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+administrator+"/project-permissions",
		map[string]any{"project": "KEEP", "permission": "channel.post", "granted": true}, founder, http.StatusBadRequest)
}
