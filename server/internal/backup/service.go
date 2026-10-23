package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/storage"
)

// Origins of an archive.
const (
	OriginManual    = "manual"
	OriginScheduled = "scheduled"
	OriginUploaded  = "uploaded"
)

// Statuses of an operation.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusRefused   = "refused"
)

// More errors a caller tells apart.
var (
	// ErrBusy is a backup or restore requested while another runs, on any node.
	ErrBusy = errors.New("another backup or restore is running")
	// ErrNotFound is an archive or operation that does not exist.
	ErrNotFound = errors.New("not found")
	// ErrNotReady is an archive still being written or uploaded, or failed.
	ErrNotReady = errors.New("that archive is not ready")
)

// staleAfter is how long a job may go without a heartbeat before any node
// decides its node has stopped and marks it failed.
const staleAfter = 2 * time.Minute

// heartbeatEvery is how often a running job says it is alive and asks
// whether it has been cancelled.
var heartbeatEvery = 5 * time.Second

// maintenanceSettle is how long a restore waits after turning maintenance on
// before writing, so every node has read it. Nodes cache it for two seconds.
var maintenanceSettle = 3 * time.Second

// MaxPieceBytes bounds one piece of an upload.
const MaxPieceBytes = 8 << 20

// Config wires a Service.
type Config struct {
	// Node and Host say which process runs a job: the node identifier is
	// per process, the host survives a restart.
	Node string
	Host string
	// ServerName and ServerVersion go into each manifest.
	ServerName    string
	ServerVersion string
	// WorkDirectory is where archives and tables are spooled.
	WorkDirectory string
	// SchedulePassphrase encrypts scheduled backups. Empty is no schedule.
	SchedulePassphrase string
	// MaxBytes, Interval and Keep are read each time, from settings.
	MaxBytes func(context.Context) int64
	Interval func(context.Context) time.Duration
	Keep     func(context.Context) int
}

// Hooks are how the layer above hears about jobs and maintenance.
type Hooks struct {
	// Finished hears about every job that ends, for the audit log.
	Finished func(ctx context.Context, op dbgen.Operation)
	// Restored runs in a replace restore's transaction before it commits, so
	// the record of it is in the restored audit log.
	Restored func(ctx context.Context, tx pgx.Tx, op dbgen.Operation) error
	// Maintenance hears when maintenance begins or ends, as this node sees it.
	Maintenance func(active bool)
}

// Service runs backups and restores as jobs.
type Service struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
	store   storage.Store
	log     *slog.Logger
	config  Config
	hooks   Hooks

	mu       sync.Mutex
	cancels  map[uuid.UUID]context.CancelFunc
	jobs     sync.WaitGroup
	cached   dbgen.Maintenance
	cachedAt time.Time
}

// NewService builds a service.
func NewService(pool *pgxpool.Pool, store storage.Store, config Config, log *slog.Logger) *Service {
	if config.MaxBytes == nil {
		config.MaxBytes = func(context.Context) int64 { return 0 }
	}
	if config.Interval == nil {
		config.Interval = func(context.Context) time.Duration { return 0 }
	}
	if config.Keep == nil {
		config.Keep = func(context.Context) int { return 0 }
	}
	return &Service{
		pool: pool, queries: dbgen.New(pool), store: store, log: log, config: config,
		cancels: map[uuid.UUID]context.CancelFunc{},
	}
}

// SetHooks says who to tell about jobs and maintenance. Called once, before
// any job runs.
func (s *Service) SetHooks(hooks Hooks) { s.hooks = hooks }

// Queries exposes the generated queries for the routes that list things.
func (s *Service) Queries() *dbgen.Queries { return s.queries }

// Store is where archives live.
func (s *Service) Store() storage.Store { return s.store }

// Wait waits for jobs this node is running, for shutdown and tests.
func (s *Service) Wait() { s.jobs.Wait() }

// Scheduled reports whether scheduled backups can run at all.
func (s *Service) Scheduled() bool { return s.config.SchedulePassphrase != "" }

func storageKey(id uuid.UUID) string { return "backups/" + id.String() + ".tdcbackup" }

func pieceKey(id uuid.UUID, piece int32) string {
	return fmt.Sprintf("backups/uploads/%s/%06d", id, piece)
}

// BackupRequest is what to back up, recorded on the operation.
type BackupRequest struct {
	Parts []Part      `json:"parts"`
	Rooms []uuid.UUID `json:"rooms,omitempty"`
}

// RestoreRequest is what to restore, recorded on the operation.
type RestoreRequest struct {
	Mode   Mode        `json:"mode"`
	Parts  []Part      `json:"parts,omitempty"`
	Rooms  []uuid.UUID `json:"rooms,omitempty"`
	DryRun bool        `json:"dryRun"`
}

/*
StartBackup claims the job lock and starts a backup on this node.

The archive row and the operation are written together, so an archive is
never listed without the job that is making it.
*/
func (s *Service) StartBackup(ctx context.Context, selection Selection, passphrase, origin, by string) (dbgen.Operation, error) {
	if err := CheckPassphrase(passphrase); err != nil {
		return dbgen.Operation{}, err
	}
	resolved, err := selection.Resolve()
	if err != nil {
		return dbgen.Operation{}, err
	}
	s.FailStale(ctx)

	request, _ := json.Marshal(BackupRequest(resolved))
	backupID := uuid.New()

	var op dbgen.Operation
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.queries.WithTx(tx)
		if _, err := q.CreateBackup(ctx, dbgen.CreateBackupParams{
			ID: backupID, Origin: origin, Status: "creating", StorageKey: storageKey(backupID),
			CreatedBy: by, Manifest: []byte("{}"),
		}); err != nil {
			return err
		}
		op, err = s.createJob(ctx, q, "backup", &backupID, by, request)
		return err
	})
	if err != nil {
		return dbgen.Operation{}, s.busy(ctx, err)
	}

	s.launch(op, func(ctx context.Context) (any, error) {
		return s.runBackup(ctx, op, backupID, resolved, passphrase, origin)
	})
	return op, nil
}

func (s *Service) createJob(ctx context.Context, q *dbgen.Queries, kind string, backupID *uuid.UUID, by string, request []byte) (dbgen.Operation, error) {
	return q.CreateOperation(ctx, dbgen.CreateOperationParams{
		ID: uuid.New(), Kind: kind, Status: StatusPending, Node: s.config.Node, Host: s.config.Host,
		BackupID: backupID, RequestedBy: by, Request: request, Result: []byte("{}"),
	})
}

// busy turns the job lock's refusal into ErrBusy naming what holds it.
func (s *Service) busy(ctx context.Context, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "operations_one_job" {
		running, readErr := s.queries.RunningJob(ctx)
		if readErr != nil {
			return ErrBusy
		}
		return fmt.Errorf("%w: a %s requested by %s at %s", ErrBusy, running.Kind,
			running.RequestedBy, running.RequestedAt.UTC().Format(time.RFC3339))
	}
	return err
}

/*
launch runs a job in the background with a heartbeat beside it.

The heartbeat is separate from progress because one large table can take
longer to read than another node will wait before deciding this one has died;
it is also how a cancellation requested on another node reaches this one.
*/
func (s *Service) launch(op dbgen.Operation, work func(context.Context) (any, error)) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[op.ID] = cancel
	s.mu.Unlock()

	var latest Progress
	var latestMu sync.Mutex
	progress := func(p Progress) error {
		latestMu.Lock()
		latest = p
		latestMu.Unlock()
		if ctx.Err() != nil {
			return ErrCancelled
		}
		return nil
	}
	ctx = context.WithValue(ctx, progressKey{}, ProgressFunc(progress))

	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		defer func() {
			s.mu.Lock()
			delete(s.cancels, op.ID)
			s.mu.Unlock()
			cancel()
		}()

		background := context.Background()
		if err := s.queries.StartOperation(background, op.ID); err != nil {
			s.log.Error("could not start an operation", "operation", op.ID, "error", err)
		}
		s.log.Info("operation started", "operation", op.ID, "kind", op.Kind, "by", op.RequestedBy)

		stopBeat := make(chan struct{})
		go func() {
			ticker := time.NewTicker(heartbeatEvery)
			defer ticker.Stop()
			for {
				select {
				case <-stopBeat:
					return
				case <-ticker.C:
					latestMu.Lock()
					encoded, _ := json.Marshal(latest)
					latestMu.Unlock()
					cancelled, err := s.queries.ReportOperationProgress(background,
						dbgen.ReportOperationProgressParams{ID: op.ID, Progress: encoded})
					if err != nil {
						s.log.Warn("could not report an operation's progress", "operation", op.ID, "error", err)
						continue
					}
					if cancelled {
						cancel()
					}
				}
			}
		}()

		result, err := s.run(ctx, work)
		close(stopBeat)

		status, message := StatusSucceeded, ""
		switch {
		case err == nil:
		case errors.Is(err, ErrCancelled) || errors.Is(err, context.Canceled):
			status, message = StatusCancelled, "cancelled"
		default:
			status, message = StatusFailed, err.Error()
		}
		encoded, _ := json.Marshal(result)
		if result == nil {
			encoded = []byte("{}")
		}
		if err := s.queries.FinishOperation(background, dbgen.FinishOperationParams{
			ID: op.ID, Status: status, Result: encoded, Error: message,
		}); err != nil {
			s.log.Error("could not record an operation's end", "operation", op.ID, "error", err)
		}

		if status == StatusSucceeded {
			s.log.Info("operation finished", "operation", op.ID, "kind", op.Kind)
		} else {
			s.log.Warn("operation did not finish", "operation", op.ID, "kind", op.Kind,
				"status", status, "error", message)
		}

		if s.hooks.Finished != nil {
			if finished, err := s.queries.GetOperation(background, op.ID); err == nil {
				s.hooks.Finished(background, finished)
			}
		}
	}()
}

// run calls work, turning a panic into a failure rather than a dead node.
func (s *Service) run(ctx context.Context, work func(context.Context) (any, error)) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("the operation stopped unexpectedly: %v", recovered)
		}
	}()
	return work(ctx)
}

type progressKey struct{}

func progressFrom(ctx context.Context) ProgressFunc {
	if report, ok := ctx.Value(progressKey{}).(ProgressFunc); ok {
		return report
	}
	return nil
}

// BackupResult is what a finished backup reports.
type BackupResult struct {
	BackupID  uuid.UUID `json:"backupId"`
	SizeBytes int64     `json:"sizeBytes"`
	Summary   Summary   `json:"summary"`
}

func (s *Service) runBackup(ctx context.Context, op dbgen.Operation, backupID uuid.UUID, selection Selection, passphrase, origin string) (any, error) {
	background := context.WithoutCancel(ctx)
	fail := func(err error) (any, error) {
		_ = s.queries.FailBackup(background, dbgen.FailBackupParams{ID: backupID, Error: err.Error()})
		return nil, err
	}

	spool, err := os.CreateTemp(s.config.WorkDirectory, "tdc-backup-*.tdcbackup")
	if err != nil {
		return fail(fmt.Errorf("backup: preparing the archive: %w", err))
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}()

	manifest, summary, err := Export(ctx, s.pool, s.store, ExportOptions{
		Selection:     selection,
		Passphrase:    passphrase,
		ServerName:    s.config.ServerName,
		ServerVersion: s.config.ServerVersion,
		MaxBytes:      s.config.MaxBytes(ctx),
		WorkDirectory: s.config.WorkDirectory,
		Progress:      progressFrom(ctx),
	}, spool)
	if err != nil {
		return fail(err)
	}

	size, err := spool.Seek(0, io.SeekCurrent)
	if err != nil {
		return fail(err)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	if err := s.store.Put(ctx, storageKey(backupID), spool, size, "application/octet-stream"); err != nil {
		return fail(fmt.Errorf("backup: storing the archive: %w", err))
	}

	encodedManifest, _ := json.Marshal(manifest)
	encodedSummary, _ := json.Marshal(summary)
	if err := s.queries.CompleteBackup(background, dbgen.CompleteBackupParams{
		ID: backupID, SizeBytes: size, Manifest: encodedManifest, Summary: encodedSummary,
	}); err != nil {
		return fail(err)
	}

	if origin == OriginScheduled {
		s.prune(background)
	}
	return BackupResult{BackupID: backupID, SizeBytes: size, Summary: summary}, nil
}

/*
StartRestore claims the job lock and restores, or previews restoring, an archive.

Applying one turns maintenance on first and waits for every node to see it.
A replace leaves maintenance on and requires a restart: running processes hold
what it changed, such as the signing key. Anything else turns it off again,
whether it succeeded or not.
*/
func (s *Service) StartRestore(ctx context.Context, backupID uuid.UUID, request RestoreRequest, passphrase, by string) (dbgen.Operation, error) {
	archive, err := s.queries.GetBackup(ctx, backupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Operation{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Operation{}, err
	}
	if archive.Status != "ready" {
		return dbgen.Operation{}, ErrNotReady
	}
	if passphrase == "" {
		return dbgen.Operation{}, fmt.Errorf("%w: the archive's passphrase is needed", ErrInvalid)
	}
	switch request.Mode {
	case ModeReplace, ModeMerge:
	default:
		return dbgen.Operation{}, fmt.Errorf("%w: %q is not a way to restore", ErrInvalid, request.Mode)
	}
	s.FailStale(ctx)

	encoded, _ := json.Marshal(request)
	op, err := s.createJob(ctx, s.queries, "restore", &backupID, by, encoded)
	if err != nil {
		return dbgen.Operation{}, s.busy(ctx, err)
	}

	s.launch(op, func(ctx context.Context) (any, error) {
		return s.runRestore(ctx, op, archive, request, passphrase)
	})
	return op, nil
}

func (s *Service) runRestore(ctx context.Context, op dbgen.Operation, archive dbgen.Backup, request RestoreRequest, passphrase string) (any, error) {
	background := context.WithoutCancel(ctx)

	if !request.DryRun {
		if err := s.SetMaintenance(background, true, "restoring a backup", &op.ID, false); err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = s.SetMaintenance(background, false, "", nil, false)
			return nil, ErrCancelled
		case <-time.After(maintenanceSettle):
		}
	}

	body, _, err := s.store.Get(ctx, archive.StorageKey)
	if err != nil {
		s.endMaintenance(background, request)
		return nil, fmt.Errorf("backup: reading the archive: %w", err)
	}
	defer func() { _ = body.Close() }()

	result, err := Restore(ctx, s.pool, s.store, body, RestoreOptions{
		Mode: request.Mode, Passphrase: passphrase, Parts: request.Parts, Rooms: request.Rooms,
		DryRun: request.DryRun, Progress: progressFrom(ctx),
		BeforeCommit: func(ctx context.Context, tx pgx.Tx) error {
			if request.Mode != ModeReplace || s.hooks.Restored == nil {
				return nil
			}
			return s.hooks.Restored(ctx, tx, op)
		},
	})
	if err != nil {
		s.endMaintenance(background, request)
		return nil, err
	}

	// An uploaded archive's manifest is not known until somebody opens it.
	if len(archive.Manifest) <= 2 {
		encoded, _ := json.Marshal(result.Manifest)
		_ = s.queries.RecordBackupManifest(background, dbgen.RecordBackupManifestParams{ID: archive.ID, Manifest: encoded})
	}

	if !request.DryRun {
		if result.RestartRequired {
			if err := s.SetMaintenance(background, true,
				"a backup was restored; restart the server to finish", &op.ID, true); err != nil {
				s.log.Error("could not record that a restart is needed", "error", err)
			}
		} else {
			s.endMaintenance(background, request)
		}
	}
	return result, nil
}

func (s *Service) endMaintenance(ctx context.Context, request RestoreRequest) {
	if request.DryRun {
		return
	}
	if err := s.SetMaintenance(ctx, false, "", nil, false); err != nil {
		s.log.Error("could not leave maintenance mode", "error", err)
	}
}

// Cancel asks a job to stop, wherever it runs.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) error {
	affected, err := s.queries.RequestOperationCancel(ctx, id)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	s.mu.Lock()
	cancel := s.cancels[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// FailStale marks jobs whose node stopped responding as failed.
func (s *Service) FailStale(ctx context.Context) {
	stale, err := s.queries.FailStaleOperations(ctx, int32(staleAfter/time.Second))
	if err != nil {
		s.log.Warn("could not check for abandoned operations", "error", err)
		return
	}
	for _, op := range stale {
		s.log.Warn("an operation's node stopped responding; marked failed", "operation", op.ID, "node", op.Node)
		if op.Kind == "backup" && op.BackupID != nil {
			_ = s.queries.FailBackup(ctx, dbgen.FailBackupParams{ID: *op.BackupID, Error: op.Error})
		}
		if op.Kind == "restore" {
			// A restore that died with maintenance on would otherwise leave it
			// on for ever. Its transaction died with its connection.
			if m, err := s.queries.GetMaintenance(ctx); err == nil && m.OperationID != nil && *m.OperationID == op.ID && !m.RestartRequired {
				_ = s.SetMaintenance(ctx, false, "", nil, false)
			}
		}
		if s.hooks.Finished != nil {
			s.hooks.Finished(ctx, op)
		}
	}
}

// Delete removes an archive and its bytes.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (dbgen.Backup, error) {
	archive, err := s.queries.GetBackup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Backup{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Backup{}, err
	}
	if running, err := s.queries.RunningJob(ctx); err == nil && running.BackupID != nil && *running.BackupID == id {
		return dbgen.Backup{}, fmt.Errorf("%w: it is in use by a %s", ErrBusy, running.Kind)
	}
	if archive.Status == "uploading" {
		for piece := int32(0); piece < archive.Pieces; piece++ {
			_ = s.store.Delete(ctx, pieceKey(id, piece))
		}
	}
	if err := s.store.Delete(ctx, archive.StorageKey); err != nil {
		return dbgen.Backup{}, fmt.Errorf("backup: deleting the archive: %w", err)
	}
	if err := s.queries.DeleteBackup(ctx, id); err != nil {
		return dbgen.Backup{}, err
	}
	return archive, nil
}

// prune deletes scheduled archives beyond the number kept.
func (s *Service) prune(ctx context.Context) {
	keep := s.config.Keep(ctx)
	if keep <= 0 {
		return
	}
	old, err := s.queries.ScheduledBackupsBeyond(ctx, int32(keep))
	if err != nil {
		s.log.Warn("could not list scheduled backups to prune", "error", err)
		return
	}
	for _, archive := range old {
		if _, err := s.Delete(ctx, archive.ID); err != nil {
			s.log.Warn("could not prune a scheduled backup", "backup", archive.ID, "error", err)
			continue
		}
		s.log.Info("pruned a scheduled backup", "backup", archive.ID, "created", archive.CreatedAt)
	}
}

/*
RunSchedule takes scheduled backups until ctx ends.

Every node runs it. Whichever claims the job lock first takes the backup; the
others are refused and look again next time, by which point the newest
scheduled archive is new.
*/
func (s *Service) RunSchedule(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.FailStale(ctx)

		interval := s.config.Interval(ctx)
		if interval <= 0 || !s.Scheduled() {
			continue
		}
		latest, err := s.queries.LatestScheduledBackup(ctx)
		if err == nil && time.Since(latest.CreatedAt) < interval {
			continue
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			s.log.Warn("could not read the last scheduled backup", "error", err)
			continue
		}
		if _, err := s.StartBackup(ctx, Selection{Parts: Parts}, s.config.SchedulePassphrase,
			OriginScheduled, "schedule"); err != nil && !errors.Is(err, ErrBusy) {
			s.log.Error("a scheduled backup could not start", "error", err)
		}
	}
}

// Maintenance reads whether the server is in maintenance, cached briefly.
func (s *Service) Maintenance(ctx context.Context) dbgen.Maintenance {
	s.mu.Lock()
	if time.Since(s.cachedAt) < 2*time.Second {
		cached := s.cached
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	current, err := s.queries.GetMaintenance(ctx)
	if err != nil {
		// Unreadable is not in maintenance: refusing every request because a
		// read failed would be an outage of its own making.
		s.log.Warn("could not read maintenance mode", "error", err)
		return dbgen.Maintenance{}
	}
	s.mu.Lock()
	changed := current.Active != s.cached.Active
	s.cached, s.cachedAt = current, time.Now()
	s.mu.Unlock()
	if changed && s.hooks.Maintenance != nil {
		s.hooks.Maintenance(current.Active)
	}
	return current
}

// SetMaintenance turns maintenance on or off.
func (s *Service) SetMaintenance(ctx context.Context, active bool, reason string, operation *uuid.UUID, restartRequired bool) error {
	if err := s.queries.SetMaintenance(ctx, dbgen.SetMaintenanceParams{
		Active: active, Reason: reason, OperationID: operation, RestartRequired: restartRequired,
	}); err != nil {
		return fmt.Errorf("backup: setting maintenance mode: %w", err)
	}
	s.log.Warn("maintenance mode changed", "active", active, "reason", reason)
	s.mu.Lock()
	s.cachedAt = time.Time{}
	s.mu.Unlock()
	s.Maintenance(ctx)
	return nil
}

// BeginUpload starts receiving an archive in pieces.
func (s *Service) BeginUpload(ctx context.Context, by string) (dbgen.Backup, error) {
	id := uuid.New()
	return s.queries.CreateBackup(ctx, dbgen.CreateBackupParams{
		ID: id, Origin: OriginUploaded, Status: "uploading", StorageKey: storageKey(id),
		CreatedBy: by, Manifest: []byte("{}"),
	})
}

/*
AddPiece stores one piece of an upload.

Pieces are stored as objects of their own rather than appended to a file, so
they can arrive at different nodes behind a load balancer. Only the next piece
in order is accepted, which makes a retried piece harmless and lets a client
resume from whatever the server says it holds.
*/
func (s *Service) AddPiece(ctx context.Context, id uuid.UUID, piece int32, data []byte) (dbgen.Backup, error) {
	if len(data) == 0 || len(data) > MaxPieceBytes {
		return dbgen.Backup{}, fmt.Errorf("%w: a piece must be between 1 byte and %d bytes", ErrInvalid, MaxPieceBytes)
	}
	archive, err := s.queries.GetBackup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Backup{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Backup{}, err
	}
	if archive.Status != "uploading" {
		return dbgen.Backup{}, ErrNotReady
	}
	if piece != archive.Pieces {
		return dbgen.Backup{}, fmt.Errorf("%w: the server holds %d pieces; send piece %d next",
			ErrInvalid, archive.Pieces, archive.Pieces)
	}
	if limit := s.config.MaxBytes(ctx); limit > 0 && archive.SizeBytes+int64(len(data)) > limit {
		return dbgen.Backup{}, fmt.Errorf("%w (%d bytes)", ErrTooLarge, limit)
	}

	if err := s.store.Put(ctx, pieceKey(id, piece), bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		return dbgen.Backup{}, fmt.Errorf("backup: storing a piece: %w", err)
	}
	updated, err := s.queries.AddBackupPiece(ctx, dbgen.AddBackupPieceParams{ID: id, Piece: piece, Size: int64(len(data))})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Backup{}, fmt.Errorf("%w: that piece has already been received", ErrInvalid)
	}
	return updated, err
}

// FinishUpload joins an upload's pieces into one archive.
func (s *Service) FinishUpload(ctx context.Context, id uuid.UUID) (dbgen.Backup, error) {
	archive, err := s.queries.GetBackup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Backup{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Backup{}, err
	}
	if archive.Status != "uploading" || archive.Pieces == 0 {
		return dbgen.Backup{}, ErrNotReady
	}

	joined := &pieces{ctx: ctx, store: s.store, id: id, count: archive.Pieces}
	defer joined.Close()
	if err := s.store.Put(ctx, archive.StorageKey, joined, archive.SizeBytes, "application/octet-stream"); err != nil {
		return dbgen.Backup{}, fmt.Errorf("backup: joining the upload: %w", err)
	}
	for piece := int32(0); piece < archive.Pieces; piece++ {
		_ = s.store.Delete(ctx, pieceKey(id, piece))
	}
	if err := s.queries.CompleteBackup(ctx, dbgen.CompleteBackupParams{
		ID: id, SizeBytes: archive.SizeBytes, Manifest: []byte("{}"), Summary: []byte("{}"),
	}); err != nil {
		return dbgen.Backup{}, err
	}
	return s.queries.GetBackup(ctx, id)
}

// pieces reads an upload's pieces one after another, opening each as needed.
type pieces struct {
	ctx     context.Context
	store   storage.Store
	id      uuid.UUID
	count   int32
	next    int32
	current io.ReadCloser
}

func (p *pieces) Read(buffer []byte) (int, error) {
	for {
		if p.current == nil {
			if p.next >= p.count {
				return 0, io.EOF
			}
			body, _, err := p.store.Get(p.ctx, pieceKey(p.id, p.next))
			if err != nil {
				return 0, err
			}
			p.current = body
			p.next++
		}
		n, err := p.current.Read(buffer)
		if errors.Is(err, io.EOF) {
			_ = p.current.Close()
			p.current = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (p *pieces) Close() {
	if p.current != nil {
		_ = p.current.Close()
	}
}
