package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/backup"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
)

const archivePassphrase = "a passphrase long enough"

// waitForOperation polls an operation until it has ended.
func waitForOperation(t *testing.T, handler http.Handler, token, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		recorder := call(t, handler, http.MethodGet, "/api/admin/operations/"+id, nil, token)
		if recorder.Code != http.StatusOK {
			t.Fatalf("operation: %d %s", recorder.Code, recorder.Body.String())
		}
		op := decode(t, recorder)
		if status := op["status"]; status != "pending" && status != "running" {
			return op
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("operation %s did not end", id)
	return nil
}

func auditActions(t *testing.T) map[string]int {
	t.Helper()
	rows, err := poolForTest.Query(context.Background(), "SELECT action FROM audit_entries")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actions := map[string]int{}
	for rows.Next() {
		var action string
		_ = rows.Scan(&action)
		actions[action]++
	}
	return actions
}

// Nobody but an administrator backs up, restores or restarts.
func TestOperationsNeedTheirPermissions(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	member := registerAndSignIn(t, handler, "member")
	someID := uuid.NewString()

	for _, attempt := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/backups"},
		{http.MethodPost, "/api/admin/backups"},
		{http.MethodGet, "/api/admin/backups/" + someID + "/download"},
		{http.MethodDelete, "/api/admin/backups/" + someID},
		{http.MethodPost, "/api/admin/backups/uploads"},
		{http.MethodPost, "/api/admin/backups/" + someID + "/restore"},
		{http.MethodGet, "/api/admin/operations"},
		{http.MethodDelete, "/api/admin/maintenance"},
		{http.MethodGet, "/api/admin/nodes"},
		{http.MethodPost, "/api/admin/nodes/this/reload"},
		{http.MethodPost, "/api/admin/nodes/this/restart"},
	} {
		body := map[string]any{"parts": []string{"settings"}, "passphrase": archivePassphrase}
		if recorder := call(t, handler, attempt.method, attempt.path, body, member); recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d, want 403", attempt.method, attempt.path, recorder.Code)
		}
	}

	var backups int
	_ = poolForTest.QueryRow(context.Background(), "SELECT COUNT(*) FROM backups").Scan(&backups)
	if backups != 0 {
		t.Error("a member's refused request still created a backup")
	}
	if recorder := call(t, handler, http.MethodGet, "/api/admin/backups", nil, admin); recorder.Code != http.StatusOK {
		t.Errorf("the administrator listing backups: %d", recorder.Code)
	}
}

/*
The whole path an administrator takes: back up, download, upload elsewhere,
preview a restore, delete — each recorded, and the archive unreadable without
the passphrase it was made with.
*/
func TestBackupDownloadUploadAndPreview(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	short := call(t, handler, http.MethodPost, "/api/admin/backups",
		map[string]any{"parts": []string{"settings"}, "passphrase": "short"}, admin)
	if short.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a short passphrase: %d %s", short.Code, short.Body.String())
	}

	started := call(t, handler, http.MethodPost, "/api/admin/backups",
		map[string]any{"parts": []string{"settings"}, "passphrase": archivePassphrase}, admin)
	if started.Code != http.StatusAccepted {
		t.Fatalf("backup: %d %s", started.Code, started.Body.String())
	}
	op := waitForOperation(t, handler, admin, decode(t, started)["id"].(string))
	if op["status"] != "succeeded" {
		t.Fatalf("backup ended %v: %v", op["status"], op["error"])
	}
	backupID := op["backupId"].(string)

	download := call(t, handler, http.MethodGet, "/api/admin/backups/"+backupID+"/download", nil, admin)
	if download.Code != http.StatusOK {
		t.Fatalf("download: %d %s", download.Code, download.Body.String())
	}
	archive := download.Body.Bytes()
	if _, err := backup.Open(bytes.NewReader(archive), "the wrong passphrase entirely"); err == nil {
		t.Error("the downloaded archive opened with the wrong passphrase")
	}
	reader, err := backup.Open(bytes.NewReader(archive), archivePassphrase)
	if err != nil {
		t.Fatalf("the downloaded archive: %v", err)
	}
	if len(reader.Manifest.Parts) != 1 || reader.Manifest.Parts[0] != backup.PartSettings {
		t.Errorf("manifest parts %v", reader.Manifest.Parts)
	}

	// Uploaded in two pieces, as from another machine.
	begun := call(t, handler, http.MethodPost, "/api/admin/backups/uploads", nil, admin)
	if begun.Code != http.StatusCreated {
		t.Fatalf("begin upload: %d %s", begun.Code, begun.Body.String())
	}
	uploadID := decode(t, begun)["id"].(string)
	half := len(archive) / 2
	for piece, data := range [][]byte{archive[:half], archive[half:]} {
		path := "/api/admin/backups/uploads/" + uploadID + "/pieces/" + string(rune('0'+piece))
		recorder := call(t, handler, http.MethodPut, path, map[string]string{"data": base64.StdEncoding.EncodeToString(data)}, admin)
		if recorder.Code != http.StatusOK {
			t.Fatalf("piece %d: %d %s", piece, recorder.Code, recorder.Body.String())
		}
	}
	// A piece sent twice is refused rather than counted twice.
	again := call(t, handler, http.MethodPut, "/api/admin/backups/uploads/"+uploadID+"/pieces/0",
		map[string]string{"data": base64.StdEncoding.EncodeToString(archive[:half])}, admin)
	if again.Code != http.StatusUnprocessableEntity {
		t.Errorf("a repeated piece: %d", again.Code)
	}
	finished := call(t, handler, http.MethodPost, "/api/admin/backups/uploads/"+uploadID+"/finish", nil, admin)
	if finished.Code != http.StatusOK {
		t.Fatalf("finish upload: %d %s", finished.Code, finished.Body.String())
	}

	preview := call(t, handler, http.MethodPost, "/api/admin/backups/"+uploadID+"/restore", map[string]any{
		"mode": "merge", "parts": []string{"settings"}, "passphrase": archivePassphrase, "dryRun": true,
	}, admin)
	if preview.Code != http.StatusAccepted {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	previewed := waitForOperation(t, handler, admin, decode(t, preview)["id"].(string))
	if previewed["status"] != "succeeded" {
		t.Fatalf("preview ended %v: %v", previewed["status"], previewed["error"])
	}

	wrong := call(t, handler, http.MethodPost, "/api/admin/backups/"+uploadID+"/restore", map[string]any{
		"mode": "merge", "parts": []string{"settings"}, "passphrase": "not the passphrase at all", "dryRun": true,
	}, admin)
	wrongOp := waitForOperation(t, handler, admin, decode(t, wrong)["id"].(string))
	if wrongOp["status"] != "failed" {
		t.Errorf("a preview with the wrong passphrase ended %v", wrongOp["status"])
	}

	if recorder := call(t, handler, http.MethodDelete, "/api/admin/backups/"+backupID, nil, admin); recorder.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", recorder.Code, recorder.Body.String())
	}

	actions := auditActions(t)
	for _, action := range []string{"server.backup.requested", "server.backup.created", "server.backup.downloaded",
		"server.backup.uploaded", "server.backup.deleted"} {
		if actions[action] == 0 {
			t.Errorf("no %s in the audit log", action)
		}
	}
}

// One backup or restore at a time, on whichever node.
func TestOneJobAtATime(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	// A job another node is running, by the look of the database.
	if _, err := poolForTest.Exec(context.Background(), `INSERT INTO operations (id, kind, status, node, requested_by)
		VALUES ($1, 'restore', 'running', 'another-node', 'someone')`, uuid.New()); err != nil {
		t.Fatal(err)
	}
	refused := call(t, handler, http.MethodPost, "/api/admin/backups",
		map[string]any{"parts": []string{"settings"}, "passphrase": archivePassphrase}, admin)
	if refused.Code != http.StatusConflict {
		t.Fatalf("a second job: %d %s", refused.Code, refused.Body.String())
	}

	// Once its node has stopped answering, the lock is released.
	if _, err := poolForTest.Exec(context.Background(),
		`UPDATE operations SET heartbeat_at = NOW() - interval '10 minutes'`); err != nil {
		t.Fatal(err)
	}
	accepted := call(t, handler, http.MethodPost, "/api/admin/backups",
		map[string]any{"parts": []string{"settings"}, "passphrase": archivePassphrase}, admin)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("after the stale job: %d %s", accepted.Code, accepted.Body.String())
	}
	waitForOperation(t, handler, admin, decode(t, accepted)["id"].(string))
}

// While a restore runs, ordinary requests are refused and health is not.
func TestMaintenanceRefusesOrdinaryRequests(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	if _, err := poolForTest.Exec(context.Background(), `UPDATE maintenance SET active = TRUE, reason = 'test'`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)

	if recorder := call(t, handler, http.MethodGet, "/api/rooms", nil, admin); recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("an ordinary request in maintenance: %d", recorder.Code)
	}
	health := call(t, handler, http.MethodGet, "/api/health", nil, "")
	if health.Code != http.StatusOK || decode(t, health)["maintenance"] != true {
		t.Errorf("health in maintenance: %d %s", health.Code, health.Body.String())
	}
	if recorder := call(t, handler, http.MethodGet, "/api/admin/backups", nil, admin); recorder.Code != http.StatusOK {
		t.Errorf("watching backups in maintenance: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodDelete, "/api/admin/maintenance", nil, admin); recorder.Code != http.StatusNoContent {
		t.Errorf("leaving maintenance: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodGet, "/api/rooms", nil, admin); recorder.Code != http.StatusOK {
		t.Errorf("after maintenance: %d", recorder.Code)
	}
}

func lifecycleFor(plan httpapi.RestartPlan, restarted *atomic.Value) httpapi.Lifecycle {
	return httpapi.Lifecycle{
		Node: "test-node", Host: "test-host", StartedAt: time.Now(),
		PlanRestart: func(context.Context) httpapi.RestartPlan { return plan },
		Describe:    func() httpapi.RestartPlan { return plan },
		Restart:     func(op uuid.UUID) { restarted.Store(op) },
		Reload: func(context.Context) (httpapi.ReloadReport, error) {
			return httpapi.ReloadReport{Applied: []string{"log.level"}, NeedsRestart: []string{"database.url"}}, nil
		},
	}
}

// A restart whose next process would not start is refused, and nothing stops.
func TestRestartIsRefusedWhenTheNextProcessWouldNotStart(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	var restarted atomic.Value
	handler.(interface{ Server() *httpapi.Server }).Server().SetLifecycle(lifecycleFor(httpapi.RestartPlan{
		Method:   "exec",
		Problems: []string{`invalid configuration: storage.backend: "floppy" is not valid`},
	}, &restarted))

	recorder := call(t, handler, http.MethodPost, "/api/admin/nodes/this/restart", nil, admin)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("restart with a bad configuration: %d %s", recorder.Code, recorder.Body.String())
	}
	if message, _ := decode(t, recorder)["message"].(string); !strings.Contains(message, "floppy") {
		t.Errorf("the refusal did not say why: %q", message)
	}
	var status string
	_ = poolForTest.QueryRow(context.Background(), "SELECT status FROM operations WHERE kind = 'restart'").Scan(&status)
	if status != "refused" {
		t.Errorf("the restart was recorded as %q", status)
	}
	time.Sleep(700 * time.Millisecond)
	if restarted.Load() != nil {
		t.Fatal("the server began restarting although the check failed")
	}
	if auditActions(t)["server.restart.refused"] == 0 {
		t.Error("the refusal was not audited")
	}

	// And a method that would only stop the server is refused the same way.
	handler.(interface{ Server() *httpapi.Server }).Server().SetLifecycle(lifecycleFor(httpapi.RestartPlan{
		Refusal: "restarting from the interface is switched off",
	}, &restarted))
	if recorder := call(t, handler, http.MethodPost, "/api/admin/nodes/this/restart", nil, admin); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("restart switched off: %d", recorder.Code)
	}
}

// A restart that passes its check is recorded, audited, and begun.
func TestRestartBegins(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	var restarted atomic.Value
	handler.(interface{ Server() *httpapi.Server }).Server().SetLifecycle(
		lifecycleFor(httpapi.RestartPlan{Method: "exec"}, &restarted))

	nodes := call(t, handler, http.MethodGet, "/api/admin/nodes", nil, admin)
	if nodes.Code != http.StatusOK {
		t.Fatalf("nodes: %d %s", nodes.Code, nodes.Body.String())
	}

	recorder := call(t, handler, http.MethodPost, "/api/admin/nodes/this/restart", nil, admin)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("restart: %d %s", recorder.Code, recorder.Body.String())
	}
	id := decode(t, recorder)["id"].(string)
	deadline := time.Now().Add(3 * time.Second)
	for restarted.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got, _ := restarted.Load().(uuid.UUID); got.String() != id {
		t.Fatalf("restart began for %v, want %s", restarted.Load(), id)
	}
	if auditActions(t)["server.restart.requested"] == 0 {
		t.Error("the restart was not audited before it began")
	}

	reload := call(t, handler, http.MethodPost, "/api/admin/nodes/this/reload", nil, admin)
	if reload.Code != http.StatusOK {
		t.Errorf("reload: %d %s", reload.Code, reload.Body.String())
	}
}
