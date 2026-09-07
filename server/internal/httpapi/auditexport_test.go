package httpapi_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// exportedAuditEntry mirrors one line of an export.
type exportedAuditEntry struct {
	ID         int64           `json:"id"`
	OccurredAt string          `json:"occurredAt"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	TargetKind string          `json:"targetKind"`
	TargetID   string          `json:"targetId"`
	Detail     json.RawMessage `json:"detail"`
}

// exportAudit reads the whole log as an operator would.
func exportAudit(t *testing.T, handler http.Handler, token string) []exportedAuditEntry {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/admin/audit/export", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("exporting returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var entries []exportedAuditEntry
	scanner := bufio.NewScanner(strings.NewReader(recorder.Body.String()))
	// A single entry can carry a long user agent and a detail object, so the
	// default line limit is not enough for a log that has seen real requests.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry exportedAuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("a line of the export is not valid JSON: %v\n%s", err, truncate(line))
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading the export: %v", err)
	}

	return entries
}

/*
TestTheExportRoundTrips is the scenario the requirement names.

An export is what an operator keeps once the log itself has been pruned, so
every field has to survive it. A format that dropped one would be a record
somebody discovers is incomplete only when they need it.
*/
func TestTheExportRoundTrips(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// Something with a target and a detail worth carrying.
	setSetting(t, handler, owner, "registration.policy", "closed")

	entries := exportAudit(t, handler, owner)
	if len(entries) == 0 {
		t.Fatal("the export is empty although the log has entries")
	}

	found := false
	for _, entry := range entries {
		if entry.Action != "server.setting.changed" {
			continue
		}
		found = true

		if entry.ID == 0 {
			t.Error("an exported entry has no identifier")
		}
		if entry.OccurredAt == "" {
			t.Error("an exported entry has no time")
		}
		if entry.Actor == "" {
			t.Error("an exported entry has no actor")
		}
		if entry.TargetKind != "setting" || entry.TargetID != "registration.policy" {
			t.Errorf("the target did not survive the export: %q %q",
				entry.TargetKind, entry.TargetID)
		}

		// The detail is JSON in its own right, not a string containing JSON:
		// an export somebody has to unescape twice is one they will parse
		// wrongly.
		var detail map[string]any
		if err := json.Unmarshal(entry.Detail, &detail); err != nil {
			t.Errorf("the detail is not usable JSON: %v", err)
		}
		if detail["to"] != "closed" {
			t.Errorf("the detail did not survive: %v", detail)
		}
	}

	if !found {
		t.Error("the setting change is missing from the export")
	}
}

/*
TestTheExportIsInOrder is what makes it a log rather than a bag of entries.

Oldest first, by the order they were written. An export whose order was
incidental would not let a reader reconstruct what happened.
*/
func TestTheExportIsInOrder(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "registration.policy", "closed")
	setSetting(t, handler, owner, "limits.max_users", "10")

	entries := exportAudit(t, handler, owner)

	for i := 1; i < len(entries); i++ {
		if entries[i].ID <= entries[i-1].ID {
			t.Fatalf("the export is not in order: %d follows %d",
				entries[i].ID, entries[i-1].ID)
		}
	}
}

/*
TestExportingIsItselfRecorded closes the obvious hole.

Taking a copy of the whole log is exactly the sort of thing somebody later
needs to account for — more so than reading a page of it, which is already
recorded.
*/
func TestExportingIsItselfRecorded(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	exportAudit(t, handler, owner)

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=20", nil, owner))

	found := false
	for _, entry := range entries {
		if entry.Action == "server.audit.exported" {
			found = true
		}
	}
	if !found {
		t.Error("exporting the whole audit log left no record of itself")
	}
}

// TestExportNeedsPermission keeps the log out of ordinary hands.
func TestExportNeedsPermission(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	ordinary := registerAndSignIn(t, handler, "amina")

	recorder := call(t, handler, http.MethodGet, "/api/admin/audit/export", nil, ordinary)
	if recorder.Code == http.StatusOK {
		t.Error("an ordinary account exported the audit log")
	}
}

// TestTheExportIsNewlineDelimited checks the format is what it claims.
//
// One entry per line, so a reader can process a log larger than its memory.
func TestTheExportIsNewlineDelimited(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "registration.policy", "closed")

	recorder := call(t, handler, http.MethodGet, "/api/admin/audit/export", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("exporting returned %d", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "ndjson") {
		t.Errorf("the export is served as %q, expected newline-delimited JSON", contentType)
	}

	body := strings.TrimSpace(recorder.Body.String())
	if strings.HasPrefix(body, "[") {
		t.Error("the export is a JSON array, which has to be held whole by " +
			"whatever reads it")
	}

	// Every line stands alone.
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("a line is not a complete entry: %v", err)
		}
	}
}
