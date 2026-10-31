package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/backup"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Operating the server: backups, restores, maintenance, reload and restart.

Three permissions, none of which anybody holds but an administrator: backing
up reads everything, restoring overwrites it, restarting interrupts everybody.
Each is rate-limited per person and audited when asked for and when it ends.
*/

func init() {
	registerRoutes(func(s *Server) {
		if s.backups == nil {
			return
		}
		admin := s.echo.Group("/api/admin", requireAuthentication)
		anyOperator := requireAnyPermission(authz.PermServerBackup, authz.PermServerRestore, authz.PermServerRestart)
		backupOrRestore := requireAnyPermission(authz.PermServerBackup, authz.PermServerRestore)

		admin.GET("/backups", s.handleListBackups, backupOrRestore)
		admin.POST("/backups", s.handleCreateBackup, requirePermission(authz.PermServerBackup))
		admin.GET("/backups/channels", s.handleBackupChannels, backupOrRestore)
		admin.GET("/backups/:id", s.handleGetBackup, backupOrRestore)
		admin.GET("/backups/:id/download", s.handleDownloadBackup, requirePermission(authz.PermServerBackup))
		admin.DELETE("/backups/:id", s.handleDeleteBackup, requirePermission(authz.PermServerBackup))
		admin.POST("/backups/uploads", s.handleBeginUpload, requirePermission(authz.PermServerBackup))
		admin.PUT("/backups/uploads/:id/pieces/:piece", s.handleUploadPiece, requirePermission(authz.PermServerBackup))
		admin.POST("/backups/uploads/:id/finish", s.handleFinishUpload, requirePermission(authz.PermServerBackup))
		admin.POST("/backups/:id/restore", s.handleRestoreBackup, requirePermission(authz.PermServerRestore))

		admin.GET("/operations", s.handleListOperations, anyOperator)
		admin.GET("/operations/:id", s.handleGetOperation, anyOperator)
		admin.POST("/operations/:id/cancel", s.handleCancelOperation, backupOrRestore)

		admin.GET("/maintenance", s.handleGetMaintenance, anyOperator)
		admin.DELETE("/maintenance", s.handleEndMaintenance, requirePermission(authz.PermServerRestore))

		admin.GET("/nodes", s.handleListNodes, requirePermission(authz.PermServerRestart))
		admin.POST("/nodes/:node/reload", s.handleReloadNode, requirePermission(authz.PermServerRestart))
		admin.POST("/nodes/:node/restart", s.handleRestartNode, requirePermission(authz.PermServerRestart))
	})

	documentRoutes(map[string]entry{
		"GET /api/admin/backups":                           {Summary: "List backups, the parts a backup can hold, and maintenance", Authentication: "session or token", Permission: "server.backup or server.restore"},
		"POST /api/admin/backups":                          {Summary: "Start a backup of chosen parts", Authentication: "session or token", Permission: "server.backup"},
		"GET /api/admin/backups/channels":                  {Summary: "Every channel, for choosing which to back up or restore", Authentication: "session or token", Permission: "server.backup or server.restore"},
		"GET /api/admin/backups/:id":                       {Summary: "A backup", Authentication: "session or token", Permission: "server.backup or server.restore"},
		"GET /api/admin/backups/:id/download":              {Summary: "Download a backup's encrypted archive", Authentication: "session or token", Permission: "server.backup"},
		"DELETE /api/admin/backups/:id":                    {Summary: "Delete a backup", Authentication: "session or token", Permission: "server.backup"},
		"POST /api/admin/backups/uploads":                  {Summary: "Begin uploading an archive", Authentication: "session or token", Permission: "server.backup"},
		"PUT /api/admin/backups/uploads/:id/pieces/:piece": {Summary: "Upload one piece of an archive", Authentication: "session or token", Permission: "server.backup"},
		"POST /api/admin/backups/uploads/:id/finish":       {Summary: "Finish uploading an archive", Authentication: "session or token", Permission: "server.backup"},
		"POST /api/admin/backups/:id/restore":              {Summary: "Restore, or preview restoring, a backup", Authentication: "session or token", Permission: "server.restore"},
		"GET /api/admin/operations":                        {Summary: "Recent backups, restores, reloads and restarts", Authentication: "session or token", Permission: "server.backup, server.restore or server.restart"},
		"GET /api/admin/operations/:id":                    {Summary: "An operation and its progress", Authentication: "session or token", Permission: "server.backup, server.restore or server.restart"},
		"POST /api/admin/operations/:id/cancel":            {Summary: "Cancel a backup or restore", Authentication: "session or token", Permission: "server.backup or server.restore"},
		"GET /api/admin/maintenance":                       {Summary: "Whether the server is in maintenance", Authentication: "session or token", Permission: "server.backup, server.restore or server.restart"},
		"DELETE /api/admin/maintenance":                    {Summary: "Leave maintenance mode", Authentication: "session or token", Permission: "server.restore"},
		"GET /api/admin/nodes":                             {Summary: "The server's nodes, who is connected, and how a restart would happen", Authentication: "session or token", Permission: "server.restart"},
		"POST /api/admin/nodes/:node/reload":               {Summary: "Reload a node's configuration", Authentication: "session or token", Permission: "server.restart"},
		"POST /api/admin/nodes/:node/restart":              {Summary: "Restart a node", Authentication: "session or token", Permission: "server.restart"},
	})
}

// Rate limits, per person.
var (
	backupsPerPerson  = cache.RateLimit{Attempts: 10, Window: time.Hour}
	restoresPerPerson = cache.RateLimit{Attempts: 20, Window: time.Hour}
	uploadsPerPerson  = cache.RateLimit{Attempts: 5, Window: time.Hour}
	restartsPerPerson = cache.RateLimit{Attempts: 10, Window: 10 * time.Minute}
)

// limited refuses somebody past a limit, saying when to try again.
func (s *Server) limited(c *echo.Context, name string, limit cache.RateLimit) error {
	if s.limiter == nil {
		return nil
	}
	user, _ := currentUser(c)
	decision, err := s.limiter.Allow(c.Request().Context(), "operations:"+name+":"+user.ID.String(), limit)
	if err != nil || decision.Allowed {
		return nil
	}
	seconds := int(decision.RetryAfter.Seconds()) + 1
	c.Response().Header().Set("Retry-After", strconv.Itoa(seconds))
	return echo.NewHTTPError(http.StatusTooManyRequests,
		fmt.Sprintf("too many of these for now; try again in %d seconds", seconds))
}

// operationError turns the backup package's errors into answers.
func operationError(err error) error {
	switch {
	case errors.Is(err, backup.ErrBusy):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, backup.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "there is no such backup or operation")
	case errors.Is(err, backup.ErrNotReady):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, backup.ErrTooLarge):
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, backup.ErrInvalid), errors.Is(err, backup.ErrWrongPassphrase), errors.Is(err, backup.ErrNotAnArchive):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}
	return err
}

func pathUUID(c *echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil(), echo.NewHTTPError(http.StatusNotFound, "there is no such backup or operation")
	}
	return id, nil
}

// backupResponse is an archive as a client sees it.
type backupResponse struct {
	ID          string          `json:"id"`
	Origin      string          `json:"origin"`
	Status      string          `json:"status"`
	SizeBytes   int64           `json:"sizeBytes"`
	Pieces      int32           `json:"pieces"`
	Manifest    json.RawMessage `json:"manifest"`
	Summary     json.RawMessage `json:"summary"`
	CreatedBy   string          `json:"createdBy"`
	CreatedAt   string          `json:"createdAt"`
	CompletedAt string          `json:"completedAt,omitempty"`
	Error       string          `json:"error,omitempty"`
}

func toBackupResponse(b dbgen.Backup) backupResponse {
	response := backupResponse{
		ID: b.ID.String(), Origin: b.Origin, Status: b.Status, SizeBytes: b.SizeBytes, Pieces: b.Pieces,
		Manifest: rawOrEmpty(b.Manifest), Summary: rawOrEmpty(b.Summary), CreatedBy: b.CreatedBy,
		CreatedAt: b.CreatedAt.UTC().Format(time.RFC3339), Error: b.Error,
	}
	if b.CompletedAt != nil {
		response.CompletedAt = b.CompletedAt.UTC().Format(time.RFC3339)
	}
	return response
}

func rawOrEmpty(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(value)
}

// operationResponse is an operation as a client sees it.
type operationResponse struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Status          string          `json:"status"`
	Node            string          `json:"node"`
	Host            string          `json:"host"`
	BackupID        string          `json:"backupId,omitempty"`
	RequestedBy     string          `json:"requestedBy"`
	RequestedAt     string          `json:"requestedAt"`
	StartedAt       string          `json:"startedAt,omitempty"`
	FinishedAt      string          `json:"finishedAt,omitempty"`
	CancelRequested bool            `json:"cancelRequested"`
	Request         json.RawMessage `json:"request"`
	Progress        json.RawMessage `json:"progress"`
	Result          json.RawMessage `json:"result"`
	Error           string          `json:"error,omitempty"`
}

func toOperationResponse(op dbgen.Operation) operationResponse {
	response := operationResponse{
		ID: op.ID.String(), Kind: op.Kind, Status: op.Status, Node: op.Node, Host: op.Host,
		RequestedBy: op.RequestedBy, RequestedAt: op.RequestedAt.UTC().Format(time.RFC3339),
		CancelRequested: op.CancelRequested, Request: rawOrEmpty(op.Request), Progress: rawOrEmpty(op.Progress),
		Result: rawOrEmpty(op.Result), Error: op.Error,
	}
	if op.BackupID != nil {
		response.BackupID = op.BackupID.String()
	}
	if op.StartedAt != nil {
		response.StartedAt = op.StartedAt.UTC().Format(time.RFC3339)
	}
	if op.FinishedAt != nil {
		response.FinishedAt = op.FinishedAt.UTC().Format(time.RFC3339)
	}
	return response
}

// partResponse is one part, for a client to offer.
type partResponse struct {
	Name              string   `json:"name"`
	Needs             []string `json:"needs"`
	NeedsEveryChannel bool     `json:"needsEveryChannel"`
	// NotPartial is why the part cannot be restored on its own, if it cannot.
	NotPartial string `json:"notPartial,omitempty"`
}

type maintenanceResponse struct {
	Active          bool   `json:"active"`
	Reason          string `json:"reason"`
	RestartRequired bool   `json:"restartRequired"`
	OperationID     string `json:"operationId,omitempty"`
	Since           string `json:"since"`
}

func toMaintenanceResponse(m dbgen.Maintenance) maintenanceResponse {
	response := maintenanceResponse{Active: m.Active, Reason: m.Reason, RestartRequired: m.RestartRequired,
		Since: m.Since.UTC().Format(time.RFC3339)}
	if m.OperationID != nil {
		response.OperationID = m.OperationID.String()
	}
	return response
}

type backupsResponse struct {
	Backups     []backupResponse    `json:"backups"`
	Parts       []partResponse      `json:"parts"`
	Running     *operationResponse  `json:"running"`
	Maintenance maintenanceResponse `json:"maintenance"`
	// Scheduled is whether a passphrase for scheduled backups is configured.
	Scheduled           bool  `json:"scheduled"`
	IntervalHours       int64 `json:"intervalHours"`
	Keep                int64 `json:"keep"`
	MaxBytes            int64 `json:"maxBytes"`
	MaxPieceBytes       int   `json:"maxPieceBytes"`
	MinPassphraseLength int   `json:"minPassphraseLength"`
}

func (s *Server) handleListBackups(c *echo.Context) error {
	ctx := c.Request().Context()
	queries := s.backups.Queries()
	s.backups.FailStale(ctx)

	archives, err := queries.ListBackups(ctx)
	if err != nil {
		return err
	}
	response := backupsResponse{
		Backups:             make([]backupResponse, 0, len(archives)),
		Maintenance:         toMaintenanceResponse(s.backups.Maintenance(ctx)),
		Scheduled:           s.backups.Scheduled(),
		IntervalHours:       s.limitFor(ctx, settings.BackupIntervalHours),
		Keep:                s.limitFor(ctx, settings.BackupKeep),
		MaxBytes:            s.limitFor(ctx, settings.BackupMaxBytes),
		MaxPieceBytes:       backup.MaxPieceBytes,
		MinPassphraseLength: backup.MinPassphraseLength,
	}
	for _, archive := range archives {
		response.Backups = append(response.Backups, toBackupResponse(archive))
	}
	for _, part := range backup.Parts {
		needs := []string{}
		for _, needed := range backup.Dependencies(part) {
			needs = append(needs, string(needed))
		}
		response.Parts = append(response.Parts, partResponse{
			Name: string(part), Needs: needs, NeedsEveryChannel: backup.NeedsEveryChannel(part),
			NotPartial: backup.NotPartial[part],
		})
	}
	if running, err := queries.RunningJob(ctx); err == nil {
		op := toOperationResponse(running)
		response.Running = &op
	}
	return c.JSON(http.StatusOK, response)
}

type backupChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// handleBackupChannels lists every channel, including ones the administrator
// is not in: a backup of a channel nobody here belongs to is still a backup.
func (s *Server) handleBackupChannels(c *echo.Context) error {
	rows, err := s.db.Query(c.Request().Context(),
		`SELECT id::text, name, kind FROM rooms WHERE kind IN ('channel', 'disposable') ORDER BY name LIMIT 2000`)
	if err != nil {
		return err
	}
	channels, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (backupChannel, error) {
		var channel backupChannel
		err := row.Scan(&channel.ID, &channel.Name, &channel.Kind)
		return channel, err
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, channels)
}

type createBackupRequest struct {
	Parts      []string `json:"parts"`
	Rooms      []string `json:"rooms"`
	Passphrase string   `json:"passphrase"`
}

func parseRooms(values []string) ([]uuid.UUID, error) {
	rooms := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("%q is not a channel", value))
		}
		rooms = append(rooms, id)
	}
	return rooms, nil
}

func parseParts(values []string) []backup.Part {
	parts := make([]backup.Part, 0, len(values))
	for _, value := range values {
		parts = append(parts, backup.Part(value))
	}
	return parts
}

func (s *Server) handleCreateBackup(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	if err := s.limited(c, "backup", backupsPerPerson); err != nil {
		return err
	}

	var request createBackupRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	rooms, err := parseRooms(request.Rooms)
	if err != nil {
		return err
	}
	selection := backup.Selection{Parts: parseParts(request.Parts), Rooms: rooms}

	op, err := s.backups.StartBackup(ctx, selection, request.Passphrase, backup.OriginManual, user.Username)
	if err != nil {
		return operationError(err)
	}
	// The passphrase is not in the entry, and not in anything else.
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: audit.ActionBackupRequested,
		TargetKind: "backup", TargetID: uuidString(op.BackupID), SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
		Detail: map[string]any{"operation": op.ID.String(), "parts": request.Parts, "rooms": len(rooms)},
	}); err != nil {
		_ = s.backups.Cancel(context.WithoutCancel(ctx), op.ID)
		return err
	}
	return c.JSON(http.StatusAccepted, toOperationResponse(op))
}

func uuidString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (s *Server) handleGetBackup(c *echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	archive, err := s.backups.Queries().GetBackup(c.Request().Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return operationError(backup.ErrNotFound)
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toBackupResponse(archive))
}

/*
handleDownloadBackup hands over an archive, already encrypted.

An object store that can sign a link is sent to directly, and honours a range
request itself. A local file is served with ranges, so an interrupted download
of a large archive resumes rather than starting again. The write deadline is
lifted for it: a download of gigabytes outlasts any timeout chosen for API
requests.
*/
func (s *Server) handleDownloadBackup(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	archive, err := s.backups.Queries().GetBackup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return operationError(backup.ErrNotFound)
	}
	if err != nil {
		return err
	}
	if archive.Status != "ready" {
		return operationError(backup.ErrNotReady)
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: audit.ActionBackupDownloaded,
		TargetKind: "backup", TargetID: id.String(), SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	name := "tdc-" + archive.CreatedAt.UTC().Format("2006-01-02-150405") + ".tdcbackup"
	store := s.backups.Store()
	if link, err := store.PresignGet(ctx, archive.StorageKey, 10*time.Minute, name); err == nil && link != "" {
		return c.Redirect(http.StatusFound, link)
	}

	body, object, err := store.Get(ctx, archive.StorageKey)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Time{})
	header := c.Response().Header()
	header.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Cache-Control", "no-store")
	if seeker, ok := body.(io.ReadSeeker); ok {
		http.ServeContent(c.Response(), c.Request(), name, object.ModifiedAt, seeker)
		return nil
	}
	header.Set("Content-Length", strconv.FormatInt(object.Size, 10))
	return c.Stream(http.StatusOK, "application/octet-stream", body)
}

func (s *Server) handleDeleteBackup(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	archive, err := s.backups.Delete(ctx, id)
	if err != nil {
		return operationError(err)
	}
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: audit.ActionBackupDeleted,
		TargetKind: "backup", TargetID: id.String(), SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
		Detail: map[string]any{"origin": archive.Origin, "sizeBytes": archive.SizeBytes},
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleBeginUpload(c *echo.Context) error {
	user, _ := currentUser(c)
	if err := s.limited(c, "upload", uploadsPerPerson); err != nil {
		return err
	}
	archive, err := s.backups.BeginUpload(c.Request().Context(), user.Username)
	if err != nil {
		return operationError(err)
	}
	return c.JSON(http.StatusCreated, toBackupResponse(archive))
}

type uploadPieceRequest struct {
	// Data is the piece, base64-encoded: the API carries JSON in both
	// clients, and a piece is at most a few megabytes.
	Data string `json:"data"`
}

func (s *Server) handleUploadPiece(c *echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	piece, err := strconv.ParseInt(c.Param("piece"), 10, 32)
	if err != nil || piece < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a piece is numbered from zero")
	}
	// Bounded before it is read: base64 is four bytes for every three.
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, int64(backup.MaxPieceBytes)*4/3+1024)
	var request uploadPieceRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "a piece is at most 8 MB")
	}
	data, err := base64.StdEncoding.DecodeString(request.Data)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "a piece is sent as base64")
	}
	archive, err := s.backups.AddPiece(c.Request().Context(), id, int32(piece), data)
	if err != nil {
		return operationError(err)
	}
	return c.JSON(http.StatusOK, toBackupResponse(archive))
}

func (s *Server) handleFinishUpload(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	archive, err := s.backups.FinishUpload(ctx, id)
	if err != nil {
		return operationError(err)
	}
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: audit.ActionBackupUploaded,
		TargetKind: "backup", TargetID: id.String(), SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
		Detail: map[string]any{"sizeBytes": archive.SizeBytes},
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toBackupResponse(archive))
}

type restoreRequest struct {
	// Mode is "replace" (everything) or "merge" (add what is missing).
	Mode       string   `json:"mode"`
	Parts      []string `json:"parts"`
	Rooms      []string `json:"rooms"`
	Passphrase string   `json:"passphrase"`
	DryRun     bool     `json:"dryRun"`
}

func (s *Server) handleRestoreBackup(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.limited(c, "restore", restoresPerPerson); err != nil {
		return err
	}
	var request restoreRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	rooms, err := parseRooms(request.Rooms)
	if err != nil {
		return err
	}
	restore := backup.RestoreRequest{
		Mode: backup.Mode(request.Mode), Parts: parseParts(request.Parts), Rooms: rooms, DryRun: request.DryRun,
	}

	op, err := s.backups.StartRestore(ctx, id, restore, request.Passphrase, user.Username)
	if err != nil {
		return operationError(err)
	}
	if !request.DryRun {
		if err := s.audit.Record(ctx, audit.Entry{
			ActorID: &user.ID, ActorLabel: user.Username, Action: audit.ActionRestoreRequested,
			TargetKind: "backup", TargetID: id.String(), SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
			Detail: map[string]any{"operation": op.ID.String(), "mode": request.Mode, "parts": request.Parts, "rooms": len(rooms)},
		}); err != nil {
			_ = s.backups.Cancel(context.WithoutCancel(ctx), op.ID)
			return err
		}
	}
	return c.JSON(http.StatusAccepted, toOperationResponse(op))
}

func (s *Server) handleListOperations(c *echo.Context) error {
	ctx := c.Request().Context()
	limit := int32(50)
	if raw := c.QueryParam("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = int32(parsed)
		}
	}
	ops, err := s.backups.Queries().ListOperations(ctx, limit)
	if err != nil {
		return err
	}
	response := make([]operationResponse, 0, len(ops))
	for _, op := range ops {
		response = append(response, toOperationResponse(op))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleGetOperation(c *echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	op, err := s.backups.Queries().GetOperation(c.Request().Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return operationError(backup.ErrNotFound)
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toOperationResponse(op))
}

func (s *Server) handleCancelOperation(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.backups.Cancel(ctx, id); err != nil {
		return operationError(err)
	}
	_ = s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: audit.ActionOperationCancel,
		TargetKind: "operation", TargetID: id.String(), SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	})
	return c.NoContent(http.StatusAccepted)
}

func (s *Server) handleGetMaintenance(c *echo.Context) error {
	return c.JSON(http.StatusOK, toMaintenanceResponse(s.backups.Maintenance(c.Request().Context())))
}

/*
handleEndMaintenance takes the server out of maintenance by hand.

For the case a restart cannot settle: every node restarted and something still
left it on. Refused while a restore is running, which would then write into a
server people are using.
*/
func (s *Server) handleEndMaintenance(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	if running, err := s.backups.Queries().RunningJob(ctx); err == nil && running.Kind == "restore" {
		return echo.NewHTTPError(http.StatusConflict, "a restore is running; maintenance ends when it does")
	}
	if err := s.backups.SetMaintenance(ctx, false, "", nil, false); err != nil {
		return err
	}
	_ = s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: audit.ActionMaintenance,
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(), Detail: map[string]any{"active": false},
	})
	return c.NoContent(http.StatusNoContent)
}

type nodeResponse struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Version     string `json:"version"`
	StartedAt   string `json:"startedAt"`
	LastSeen    string `json:"lastSeen"`
	Connections int32  `json:"connections"`
	// Alive is whether it was heard from in the last minute.
	Alive bool `json:"alive"`
	// This is the node answering.
	This bool `json:"this"`
}

type nodesResponse struct {
	Nodes []nodeResponse `json:"nodes"`
	// Calls is how many calls are in progress, on any node.
	Calls int64 `json:"calls"`
	// Restart is how this node would restart, or why it cannot.
	Restart RestartPlan `json:"restart"`
}

func (s *Server) handleListNodes(c *echo.Context) error {
	ctx := c.Request().Context()
	queries := s.backups.Queries()
	nodes, err := queries.ListNodes(ctx)
	if err != nil {
		return err
	}
	calls, _ := queries.CountOpenCalls(ctx)

	response := nodesResponse{Nodes: make([]nodeResponse, 0, len(nodes)), Calls: calls}
	for _, node := range nodes {
		response.Nodes = append(response.Nodes, nodeResponse{
			ID: node.ID, Host: node.Host, Version: node.Version,
			StartedAt: node.StartedAt.UTC().Format(time.RFC3339), LastSeen: node.LastSeen.UTC().Format(time.RFC3339),
			Connections: node.Connections, Alive: time.Since(node.LastSeen) < time.Minute,
			This: node.ID == s.lifecycle.Node,
		})
	}
	// How this node would restart, without running the check: that takes a
	// process and a database connection, and is done when it is asked for.
	if s.lifecycle.PlanRestart == nil {
		response.Restart.Refusal = "this server was not started in a way that can restart itself"
	} else {
		response.Restart = s.planWithoutCheck()
	}
	return c.JSON(http.StatusOK, response)
}

// planWithoutCheck is the restart plan as the configuration describes it.
func (s *Server) planWithoutCheck() RestartPlan {
	if s.lifecycle.Describe != nil {
		return s.lifecycle.Describe()
	}
	return RestartPlan{}
}

// nodeOperation records a reload or restart for a node, running it here if
// this is that node and leaving it for that node to pick up if not.
func (s *Server) nodeOperation(c *echo.Context, kind string) (dbgen.Operation, bool, error) {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	target := c.Param("node")
	here := target == s.lifecycle.Node || target == "this"
	if here {
		target = s.lifecycle.Node
	}

	host := s.lifecycle.Host
	if !here {
		node, err := s.backups.Queries().GetNode(ctx, target)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && time.Since(node.LastSeen) > time.Minute) {
			return dbgen.Operation{}, false, echo.NewHTTPError(http.StatusNotFound, "there is no such node running")
		}
		if err != nil {
			return dbgen.Operation{}, false, err
		}
		host = node.Host
	}

	status := backup.StatusPending
	var started *time.Time
	if here {
		now := time.Now()
		status, started = backup.StatusRunning, &now
	}
	op, err := s.backups.Queries().CreateOperation(ctx, dbgen.CreateOperationParams{
		ID: uuid.New(), Kind: kind, Status: status, Node: target, Host: host,
		RequestedBy: user.Username, Request: []byte("{}"), StartedAt: started, Result: []byte("{}"),
	})
	return op, here, err
}

func (s *Server) handleReloadNode(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := s.limited(c, "restart", restartsPerPerson); err != nil {
		return err
	}
	op, here, err := s.nodeOperation(c, "reload")
	if err != nil {
		return err
	}
	if !here {
		return c.JSON(http.StatusAccepted, toOperationResponse(op))
	}
	report := s.performReload(ctx, op)
	finished, err := s.backups.Queries().GetOperation(ctx, op.ID)
	if err != nil {
		return err
	}
	// Refusals are errors, so either client shows the reason as it shows any.
	if len(report.Problems) > 0 {
		return echo.NewHTTPError(http.StatusUnprocessableEntity,
			"the configuration has problems, so nothing was reloaded: "+strings.Join(report.Problems, "; "))
	}
	return c.JSON(http.StatusOK, toOperationResponse(finished))
}

func (s *Server) handleRestartNode(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := s.limited(c, "restart", restartsPerPerson); err != nil {
		return err
	}
	op, here, err := s.nodeOperation(c, "restart")
	if err != nil {
		return err
	}
	if !here {
		return c.JSON(http.StatusAccepted, toOperationResponse(op))
	}

	impact := map[string]any{}
	if s.hub != nil {
		impact["connections"] = s.hub.Count()
	}
	if calls, err := s.backups.Queries().CountOpenCalls(ctx); err == nil {
		impact["calls"] = calls
	}
	_, err = s.performRestart(ctx, op, impact)
	finished, readErr := s.backups.Queries().GetOperation(ctx, op.ID)
	if readErr != nil {
		return readErr
	}
	if err != nil && !isRestartRefused(err) {
		return err
	}
	if isRestartRefused(err) {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "the restart was refused: "+finished.Error)
	}
	return c.JSON(http.StatusAccepted, toOperationResponse(finished))
}

/*
refuseDuringMaintenance answers ordinary requests while a restore runs.

What still answers: health (so an orchestrator does not kill the node doing the
restore), the server's identity, signing in and the signed-in person (so an
administrator signed out by a replace can get back), and the endpoints an
administrator watches the restore from. The web application itself still
loads, so its screens can say what is happening.
*/
func (s *Server) refuseDuringMaintenance(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.backups == nil {
			return next(c)
		}
		path := c.Request().URL.Path
		if !strings.HasPrefix(path, "/api/") || maintenanceExempt(path) {
			return next(c)
		}
		maintenance := s.backups.Maintenance(c.Request().Context())
		if !maintenance.Active {
			return next(c)
		}
		c.Response().Header().Set("Retry-After", "30")
		message := "the server is in maintenance while a backup is restored; try again in a few minutes"
		if maintenance.RestartRequired {
			message = "a backup was restored and the server is waiting to be restarted; try again shortly"
		}
		return echo.NewHTTPError(http.StatusServiceUnavailable, message)
	}
}

func maintenanceExempt(path string) bool {
	for _, exact := range []string{"/api/health", "/api/server", "/api/version", "/api/description",
		"/api/openapi.json", "/api/me", "/api/me/permissions", "/api/setup", "/api/admin/maintenance"} {
		if path == exact {
			return true
		}
	}
	for _, prefix := range []string{"/api/auth/", "/api/admin/backups", "/api/admin/operations", "/api/admin/nodes"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
