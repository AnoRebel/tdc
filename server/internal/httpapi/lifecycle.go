package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/backup"
	"github.com/AnoRebel/tdc/server/internal/buildinfo"
	"github.com/AnoRebel/tdc/server/internal/config"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Lifecycle is what the command gives this layer to reload and restart its own
process.

Functions rather than an interface with one implementation: the command owns
the configuration, the logger and the context whose cancelling shuts the
process down, and these are the three things it lends out. A server built
without them — every test, mostly — answers that it cannot reload or restart.
*/
type Lifecycle struct {
	// Node identifies this process; Host survives its restart.
	Node      string
	Host      string
	StartedAt time.Time
	// Reload reads the configuration again and applies what it can.
	Reload func(ctx context.Context) (ReloadReport, error)
	// PlanRestart decides how a restart would happen and checks the next
	// process would start, without stopping anything.
	PlanRestart func(ctx context.Context) RestartPlan
	// Describe is how a restart would happen, without checking the next
	// process: what a page shows before anybody asks.
	Describe func() RestartPlan
	// Restart begins shutting down to come back, completing operation.
	Restart func(operation uuid.UUID)
	// ResumedOperation is the restart this process completes, when it was
	// told by the process it replaced.
	ResumedOperation uuid.UUID
}

// ReloadReport is what a reload did.
type ReloadReport struct {
	Applied []string `json:"applied"`
	// NeedsRestart are settings that changed and take effect only on restart.
	NeedsRestart []string `json:"needsRestart"`
	// Problems are why the configuration could not be used; nothing changed.
	Problems []string `json:"problems,omitempty"`
}

// RestartPlan is how a restart would happen, or why it would not.
type RestartPlan struct {
	// Method is "exec" or "supervisor".
	Method string `json:"method"`
	// Supervisor names what will start the process again, if anything does.
	Supervisor string `json:"supervisor,omitempty"`
	// Recovers says whether something restarts the process if the new one
	// fails to start. False under exec without a supervisor.
	Recovers bool `json:"recovers"`
	// Refusal is why restarting is not possible here, when it is not.
	Refusal string `json:"refusal,omitempty"`
	// Problems are what the check of the next process found.
	Problems []string `json:"problems,omitempty"`
}

// live holds what a reload changes while the server runs.
type live struct {
	turnSecret    string
	configuration []config.Setting
	warnings      []string
}

// SetLifecycle lends this server the command's reload and restart.
func (s *Server) SetLifecycle(lifecycle Lifecycle) { s.lifecycle = lifecycle }

func (s *Server) liveConfig() live {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	return s.live
}

/*
Reconfigure applies values a reload read: the call relay's secret, and the
configuration and warnings an administrator is shown. Each takes effect on the
next request that reads it; nothing in flight is disturbed.
*/
func (s *Server) Reconfigure(turnSecret string, described []config.Setting, warnings []string) {
	s.liveMu.Lock()
	s.live = live{turnSecret: turnSecret, configuration: described, warnings: warnings}
	s.liveMu.Unlock()
}

/*
ReloadWebShell reads the application shell from the web root again.

The assets beside it are read from disk per request already; the shell is held
in memory, so deploying a new web build without this, or a restart, serves the
old shell — which names asset files that no longer exist.
*/
func (s *Server) ReloadWebShell() (bool, error) {
	root := s.config.WebRoot
	if root == "" || s.shell.Load() == nil {
		return false, nil
	}
	shell, err := fs.ReadFile(os.DirFS(root), "index.html")
	if err != nil {
		return false, err
	}
	s.shell.Store(&shell)
	return true, nil
}

/*
runNodeLoop says this node is alive, and does what other nodes asked of it.

Every three seconds it looks for a reload or restart addressed to it; every
fifteen it records itself, with how many live connections it holds. A node not
seen for a minute is shown as gone, and forgotten after a day.
*/
func (s *Server) runNodeLoop(ctx context.Context) {
	if s.backups == nil || s.lifecycle.Node == "" {
		return
	}
	queries := s.backups.Queries()

	touch := func() {
		connections := 0
		if s.hub != nil {
			connections = s.hub.Count()
		}
		if err := queries.TouchNode(ctx, dbgen.TouchNodeParams{
			ID: s.lifecycle.Node, Host: s.lifecycle.Host, Version: buildinfo.Version,
			StartedAt: s.lifecycle.StartedAt, Connections: int32(connections),
		}); err != nil && ctx.Err() == nil {
			s.log.Warn("could not record this node", "error", err)
		}
	}
	touch()
	_ = queries.ForgetNodes(ctx, dbgen.ForgetNodesParams{AfterSeconds: 24 * 60 * 60, ID: ""})

	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	look := time.NewTicker(3 * time.Second)
	defer look.Stop()
	for {
		select {
		case <-ctx.Done():
			// Gone, rather than left to look alive for a minute.
			_ = queries.ForgetNodes(context.WithoutCancel(ctx), dbgen.ForgetNodesParams{
				AfterSeconds: 24 * 60 * 60, ID: s.lifecycle.Node,
			})
			return
		case <-beat.C:
			touch()
			// Read here as well as per request, so a node nobody is using
			// still closes its connections when maintenance begins.
			s.backups.Maintenance(ctx)
		case <-look.C:
			pending, err := queries.PendingOperationsFor(ctx, s.lifecycle.Node)
			if err != nil {
				continue
			}
			for _, op := range pending {
				claimed, err := queries.ClaimOperation(ctx, op.ID)
				if err != nil || claimed == 0 {
					continue
				}
				s.log.Info("acting on a request from another node", "operation", op.ID, "kind", op.Kind,
					"by", op.RequestedBy)
				switch op.Kind {
				case "reload":
					s.performReload(ctx, op)
				case "restart":
					_, _ = s.performRestart(ctx, op, nil)
				}
			}
		}
	}
}

/*
completeRestart records that this process is the one a restart was waiting
for, once it is listening.

Told directly by the process it replaced, or found by host: a supervisor
starts a process with nothing passed from the old one, and the host is what
stays the same. A restart finished leaves maintenance a restore left on, too,
since a restart is what that was waiting for.
*/
func (s *Server) completeRestart(ctx context.Context) {
	if s.backups == nil {
		return
	}
	queries := s.backups.Queries()

	var waiting []dbgen.Operation
	if s.lifecycle.ResumedOperation != uuid.Nil() {
		if op, err := queries.GetOperation(ctx, s.lifecycle.ResumedOperation); err == nil && op.Status == backup.StatusRunning {
			waiting = append(waiting, op)
		}
	} else if s.lifecycle.Host != "" {
		waiting, _ = queries.RestartsAwaitingReturn(ctx, s.lifecycle.Host)
	}

	for _, op := range waiting {
		result, _ := json.Marshal(map[string]any{
			"node": s.lifecycle.Node, "version": buildinfo.Version,
			"startedAt": s.lifecycle.StartedAt.UTC().Format(time.RFC3339), "pid": os.Getpid(),
		})
		if err := queries.FinishOperation(ctx, dbgen.FinishOperationParams{
			ID: op.ID, Status: backup.StatusSucceeded, Result: result,
		}); err != nil {
			s.log.Error("could not record that the restart completed", "operation", op.ID, "error", err)
			continue
		}
		s.log.Info("restart completed", "operation", op.ID, "node", s.lifecycle.Node, "version", buildinfo.Version)
		_ = s.audit.Record(ctx, audit.Entry{
			ActorLabel: op.RequestedBy, Action: audit.ActionRestartCompleted,
			TargetKind: "node", TargetID: s.lifecycle.Node,
			Detail: map[string]any{"operation": op.ID.String(), "version": buildinfo.Version, "previousNode": op.Node},
		})
	}

	maintenance, err := queries.GetMaintenance(ctx)
	if err == nil && maintenance.Active && maintenance.RestartRequired && maintenance.Since.Before(s.lifecycle.StartedAt) {
		if err := s.backups.SetMaintenance(ctx, false, "", nil, false); err == nil {
			s.log.Info("left the maintenance a restore needed a restart to end")
			_ = s.audit.Record(ctx, audit.Entry{ActorLabel: "system", Action: audit.ActionMaintenance,
				Detail: map[string]any{"active": false, "reason": "restarted after a restore"}})
		}
	}
}

// performReload reloads this node for an operation and records the outcome.
func (s *Server) performReload(ctx context.Context, op dbgen.Operation) ReloadReport {
	queries := s.backups.Queries()
	status, message := backup.StatusSucceeded, ""
	var report ReloadReport
	if s.lifecycle.Reload == nil {
		status, message = backup.StatusRefused, "this server was not started in a way that can reload"
	} else {
		var err error
		report, err = s.lifecycle.Reload(ctx)
		switch {
		case err != nil:
			status, message = backup.StatusFailed, err.Error()
		case len(report.Problems) > 0:
			status, message = backup.StatusRefused, "the configuration has problems; nothing changed"
		}
	}
	result, _ := json.Marshal(report)
	if err := queries.FinishOperation(ctx, dbgen.FinishOperationParams{
		ID: op.ID, Status: status, Result: result, Error: message,
	}); err != nil {
		s.log.Error("could not record a reload", "operation", op.ID, "error", err)
	}
	s.log.Info("reloaded", "operation", op.ID, "status", status, "applied", report.Applied,
		"needsRestart", report.NeedsRestart, "problems", report.Problems)
	_ = s.audit.Record(ctx, audit.Entry{
		ActorLabel: op.RequestedBy, Action: audit.ActionReloaded, TargetKind: "node", TargetID: s.lifecycle.Node,
		Detail: map[string]any{"operation": op.ID.String(), "status": status, "applied": report.Applied,
			"needsRestart": report.NeedsRestart, "problems": len(report.Problems)},
	})
	return report
}

/*
performRestart checks a restart would succeed and, if it would, begins it.

The check runs first and refuses with what it found: a configuration the next
process cannot start with, or a method that would only stop this one. Then the
restart is recorded and audited, and only then does shutdown begin — after a
moment, so whoever asked hears the answer before the connection closes.
*/
func (s *Server) performRestart(ctx context.Context, op dbgen.Operation, impact map[string]any) (RestartPlan, error) {
	queries := s.backups.Queries()

	var plan RestartPlan
	if s.lifecycle.PlanRestart == nil || s.lifecycle.Restart == nil {
		plan.Refusal = "this server was not started in a way that can restart itself"
	} else {
		plan = s.lifecycle.PlanRestart(ctx)
	}

	if plan.Refusal != "" || len(plan.Problems) > 0 {
		message := plan.Refusal
		if message == "" {
			message = "the next process would not start: " + strings.Join(plan.Problems, "; ")
		}
		result, _ := json.Marshal(plan)
		_ = queries.FinishOperation(ctx, dbgen.FinishOperationParams{
			ID: op.ID, Status: backup.StatusRefused, Result: result, Error: message,
		})
		s.log.Warn("restart refused", "operation", op.ID, "reason", message)
		_ = s.audit.Record(ctx, audit.Entry{
			ActorLabel: op.RequestedBy, Action: audit.ActionRestartRefused, TargetKind: "node", TargetID: s.lifecycle.Node,
			Detail: map[string]any{"operation": op.ID.String(), "reason": message},
		})
		return plan, restartRefusedError{message: message}
	}

	encoded, _ := json.Marshal(plan)
	if _, err := s.db.Exec(ctx, `UPDATE operations SET request = request || $2::jsonb WHERE id = $1`,
		op.ID, encoded); err != nil {
		s.log.Warn("could not record the restart plan", "error", err)
	}
	detail := map[string]any{"operation": op.ID.String(), "method": plan.Method, "supervisor": plan.Supervisor}
	for key, value := range impact {
		detail[key] = value
	}
	if err := s.audit.Record(ctx, audit.Entry{
		ActorLabel: op.RequestedBy, Action: audit.ActionRestartRequested, TargetKind: "node", TargetID: s.lifecycle.Node,
		Detail: detail,
	}); err != nil {
		// Refused rather than performed unrecorded.
		_ = queries.FinishOperation(ctx, dbgen.FinishOperationParams{
			ID: op.ID, Status: backup.StatusFailed, Result: []byte("{}"), Error: err.Error(),
		})
		return plan, err
	}

	s.log.Warn("restarting", "operation", op.ID, "method", plan.Method, "supervisor", plan.Supervisor,
		"by", op.RequestedBy)
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.lifecycle.Restart(op.ID)
	}()
	return plan, nil
}

type restartRefusedError struct{ message string }

func (e restartRefusedError) Error() string { return e.message }

// isRestartRefused reports a refusal, as distinct from a failure.
func isRestartRefused(err error) bool {
	var refused restartRefusedError
	return errors.As(err, &refused)
}

// auditOperation records how a backup or restore ended.
func (s *Server) auditOperation(ctx context.Context, op dbgen.Operation) {
	var action audit.Action
	switch {
	case op.Kind == "backup" && op.Status == backup.StatusSucceeded:
		action = audit.ActionBackupCreated
	case op.Kind == "backup":
		action = audit.ActionBackupFailed
	case op.Kind == "restore" && op.Status == backup.StatusSucceeded:
		var request backup.RestoreRequest
		_ = json.Unmarshal(op.Request, &request)
		if request.Mode == backup.ModeReplace && !request.DryRun {
			// Recorded inside the restore itself, in the log it restored.
			return
		}
		if request.DryRun {
			return
		}
		action = audit.ActionRestoreCompleted
	case op.Kind == "restore":
		var request backup.RestoreRequest
		_ = json.Unmarshal(op.Request, &request)
		if request.DryRun {
			return
		}
		action = audit.ActionRestoreFailed
	default:
		return
	}

	detail := map[string]any{"operation": op.ID.String(), "status": op.Status}
	if op.Error != "" {
		detail["error"] = op.Error
	}
	target := ""
	if op.BackupID != nil {
		target = op.BackupID.String()
	}
	if err := s.audit.Record(ctx, audit.Entry{
		ActorLabel: op.RequestedBy, Action: action, TargetKind: "backup", TargetID: target, Detail: detail,
	}); err != nil {
		s.log.Error("could not audit an operation's end", "operation", op.ID, "error", err)
	}
}

// RecordRestoreInTransaction writes a replace restore's audit entry into the
// log it restored, before the restore commits.
func (s *Server) RecordRestoreInTransaction(ctx context.Context, tx pgx.Tx, op dbgen.Operation) error {
	target := ""
	if op.BackupID != nil {
		target = op.BackupID.String()
	}
	recorder := audit.NewRecorder(dbgen.New(tx), s.log)
	return recorder.Record(ctx, audit.Entry{
		ActorLabel: op.RequestedBy, Action: audit.ActionRestoreCompleted, TargetKind: "backup", TargetID: target,
		Detail: map[string]any{"operation": op.ID.String(), "mode": string(backup.ModeReplace)},
	})
}

// onMaintenance closes this node's live connections when maintenance begins,
// so nobody keeps a view of the server a restore is about to replace.
func (s *Server) onMaintenance(active bool) {
	if active && s.hub != nil {
		s.log.Warn("maintenance began; closing live connections")
		s.hub.CloseAll()
	}
}

// ReloadFromSignal reloads this node for a SIGHUP, recording it as any other
// reload is recorded.
func (s *Server) ReloadFromSignal(ctx context.Context) {
	if s.backups == nil {
		if s.lifecycle.Reload != nil {
			report, err := s.lifecycle.Reload(ctx)
			s.log.Info("reloaded", "applied", report.Applied, "needsRestart", report.NeedsRestart,
				"problems", report.Problems, "error", err)
		}
		return
	}
	now := time.Now()
	op, err := s.backups.Queries().CreateOperation(ctx, dbgen.CreateOperationParams{
		ID: uuid.New(), Kind: "reload", Status: backup.StatusRunning, Node: s.lifecycle.Node,
		Host: s.lifecycle.Host, RequestedBy: "SIGHUP", Request: []byte("{}"), StartedAt: &now, Result: []byte("{}"),
	})
	if err != nil {
		s.log.Error("could not record a reload", "error", err)
		return
	}
	s.performReload(ctx, op)
}
