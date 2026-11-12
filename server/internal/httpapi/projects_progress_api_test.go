package httpapi_test

import (
	"net/http"
	"testing"
)

// Progress is counted on every request: a move is in the very next answer,
// and today's point of the trend — replayed from the history — agrees with it.
func TestProgressIsCountedOnEveryRequest(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	project := startProject(t, handler, founder, "PACE", "public")
	for _, title := range []string{"One", "Two", "Three", "Four"} {
		fileIssue(t, handler, founder, "PACE", title)
	}
	done := stateNamed(t, project, "Done")

	read := func() (float64, float64) {
		t.Helper()
		progress := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/PACE/progress", nil, founder, http.StatusOK))
		trend := progress["trend"].([]any)
		if len(trend) != 14 {
			t.Errorf("two weeks of trend has %d points", len(trend))
		}
		today := trend[len(trend)-1].(map[string]any)
		if today["total"] != progress["total"] || today["completed"] != progress["completed"] {
			t.Errorf("the history says %v of %v, the issues %v of %v",
				today["completed"], today["total"], progress["completed"], progress["total"])
		}
		return progress["completed"].(float64), progress["total"].(float64)
	}

	if completed, total := read(); completed != 0 || total != 4 {
		t.Errorf("before any move: %v of %v", completed, total)
	}
	mustCall(t, handler, http.MethodPost, "/api/issues/PACE-2/state", map[string]any{"stateId": done}, founder, http.StatusOK)
	if completed, total := read(); completed != 1 || total != 4 {
		t.Errorf("after one move: %v of %v", completed, total)
	}
	mustCall(t, handler, http.MethodGet, "/api/projects/PACE/progress?from=2026-10-10&to=2026-10-01",
		nil, founder, http.StatusBadRequest)
}
