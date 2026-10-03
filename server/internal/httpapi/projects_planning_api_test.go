package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
Cycles and milestones through the API: days as days, instants with the
project's offset, issues planned one at a time or in a batch, and nobody
outside the project planning its work.
*/
func TestPlanningThroughTheAPI(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	outsider := registerMember(t, handler, "outsider")
	startProject(t, handler, founder, "PLAN", "public")
	fileIssue(t, handler, founder, "PLAN", "First")
	fileIssue(t, handler, founder, "PLAN", "Second")

	// Without any cycle an issue says nothing about one.
	plain := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/PLAN-1", nil, founder, http.StatusOK))
	if _, present := plain["cycle"]; present {
		t.Errorf("an issue in a project without cycles names one: %v", plain["cycle"])
	}

	mustCall(t, handler, http.MethodPut, "/api/projects/PLAN/planning",
		map[string]any{"timeZone": "Mars/Olympus_Mons"}, founder, http.StatusBadRequest)
	project := decode(t, mustCall(t, handler, http.MethodPut, "/api/projects/PLAN/planning",
		map[string]any{"timeZone": "Africa/Dar_es_Salaam", "cyclePolicy": "remain"}, founder, http.StatusOK))
	if project["timeZone"] != "Africa/Dar_es_Salaam" || project["cyclePolicy"] != "remain" {
		t.Errorf("planning reads %v %v", project["timeZone"], project["cyclePolicy"])
	}

	cycle := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/PLAN/cycles", map[string]any{
		"name": "Sprint 1", "startsOn": "2026-11-02", "endsOn": "2026-11-15",
	}, founder, http.StatusCreated))
	if cycle["startsOn"] != "2026-11-02" || cycle["endsOn"] != "2026-11-15" ||
		cycle["startsAt"] != "2026-11-02T00:00:00+03:00" || cycle["endsAt"] != "2026-11-16T00:00:00+03:00" {
		t.Errorf("the cycle reads %v", cycle)
	}
	id := cycle["id"].(string)
	mustCall(t, handler, http.MethodPost, "/api/projects/PLAN/cycles", map[string]any{
		"name": "Overlapping", "startsOn": "2026-11-10", "endsOn": "2026-11-20",
	}, founder, http.StatusConflict)
	mustCall(t, handler, http.MethodPost, "/api/projects/PLAN/cycles", map[string]any{
		"name": "Backwards", "startsOn": "2026-12-10", "endsOn": "2026-12-01",
	}, founder, http.StatusBadRequest)

	issue := decode(t, mustCall(t, handler, http.MethodPut, "/api/issues/PLAN-1/cycle",
		map[string]any{"cycleId": id}, founder, http.StatusOK))
	if named, _ := issue["cycle"].(map[string]any); named["name"] != "Sprint 1" {
		t.Errorf("the issue's cycle reads %v", issue["cycle"])
	}
	recorder := call(t, handler, http.MethodPut, "/api/issues/PLAN-2/cycle", map[string]any{"cycleId": id}, outsider)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "issue.plan") {
		t.Errorf("an outsider planning: %d %s", recorder.Code, recorder.Body.String())
	}
	mustCall(t, handler, http.MethodPost, "/api/projects/PLAN/cycles", map[string]any{
		"name": "Theirs", "startsOn": "2027-01-04", "endsOn": "2027-01-17",
	}, outsider, http.StatusForbidden)

	// A batch: one moves in, one already was.
	results := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/PLAN/issues/bulk", map[string]any{
		"issues": []string{"PLAN-1", "PLAN-2"}, "change": map[string]any{"kind": "cycle", "value": id},
	}, founder, http.StatusOK))["results"].([]any)
	if results[0].(map[string]any)["code"] != "unchanged" || results[1].(map[string]any)["code"] != "changed" {
		t.Errorf("the batch reads %v", results)
	}

	detail := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/PLAN/cycles/"+id, nil, founder, http.StatusOK))
	if inside := detail["issues"].([]any); len(inside) != 2 {
		t.Errorf("the cycle holds %d issues", len(inside))
	}
	if summary := detail["cycle"].(map[string]any); summary["total"] != float64(2) || summary["completed"] != float64(0) {
		t.Errorf("the cycle counts %v of %v", summary["completed"], summary["total"])
	}
	mustCall(t, handler, http.MethodDelete, "/api/projects/PLAN/cycles/"+id, nil, founder, http.StatusConflict)

	// The board narrows to a cycle, or to what is in none.
	fileIssue(t, handler, founder, "PLAN", "Unplanned")
	cards := func(query string) int {
		board := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/PLAN/board?"+query, nil, founder, http.StatusOK))
		count := 0
		for _, column := range board["columns"].([]any) {
			count += len(column.(map[string]any)["cards"].([]any))
		}
		return count
	}
	if in, none, all := cards("cycle="+id), cards("cycle=none"), cards(""); in != 2 || none != 1 || all != 3 {
		t.Errorf("the board holds %d in the cycle, %d in none, %d in all", in, none, all)
	}

	milestone := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/PLAN/milestones", map[string]any{
		"name": "Beta", "targetDate": "2026-01-31",
	}, founder, http.StatusCreated))
	if milestone["dueAt"] != "2026-02-01T00:00:00+03:00" {
		t.Errorf("the milestone is due at %v", milestone["dueAt"])
	}
	mustCall(t, handler, http.MethodPut, "/api/issues/PLAN-2/milestone",
		map[string]any{"milestoneId": milestone["id"]}, founder, http.StatusOK)
	read := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/PLAN/milestones/"+milestone["id"].(string),
		nil, founder, http.StatusOK))
	if summary := read["milestone"].(map[string]any); summary["overdue"] != true || summary["total"] != float64(1) {
		t.Errorf("a milestone past its day with work open reads %v", summary)
	}
	if spans := read["cycles"].([]any); len(spans) != 1 || spans[0].(map[string]any)["name"] != "Sprint 1" {
		t.Errorf("the milestone spans %v", spans)
	}
	if types := historyTypes(t, handler, founder, "PLAN-2"); types != "issue.created,issue.cycle_changed,issue.milestone_changed" {
		t.Errorf("the history reads %s", types)
	}
}
