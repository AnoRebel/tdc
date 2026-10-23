-- Backups, restores, reloads and restarts: see 000429_operations.

-- name: CreateBackup :one
INSERT INTO backups (id, origin, status, storage_key, created_by, manifest)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBackup :one
SELECT * FROM backups WHERE id = $1;

-- name: ListBackups :many
SELECT * FROM backups ORDER BY created_at DESC LIMIT 200;

-- name: CompleteBackup :exec
UPDATE backups
SET status = 'ready', size_bytes = $2, manifest = $3, summary = $4, completed_at = NOW(), error = ''
WHERE id = $1;

-- name: FailBackup :exec
UPDATE backups SET status = 'failed', error = $2, completed_at = NOW() WHERE id = $1;

-- name: RecordBackupManifest :exec
UPDATE backups SET manifest = $2 WHERE id = $1;

-- name: AddBackupPiece :one
-- Accepts a piece only in order, so two requests for one piece cannot both
-- be counted and a gap cannot be skipped over.
UPDATE backups
SET pieces = pieces + 1, size_bytes = size_bytes + sqlc.arg(size)::bigint
WHERE id = sqlc.arg(id) AND status = 'uploading' AND pieces = sqlc.arg(piece)::int
RETURNING *;

-- name: DeleteBackup :exec
DELETE FROM backups WHERE id = $1;

-- name: ScheduledBackupsBeyond :many
-- Scheduled archives past the newest few, for retention. Archives made by
-- hand or uploaded are never returned.
SELECT * FROM backups
WHERE origin = 'scheduled' AND status = 'ready'
ORDER BY created_at DESC
OFFSET $1;

-- name: LatestScheduledBackup :one
SELECT * FROM backups
WHERE origin = 'scheduled' AND status IN ('ready', 'creating')
ORDER BY created_at DESC
LIMIT 1;

-- name: CreateOperation :one
INSERT INTO operations (id, kind, status, node, host, backup_id, requested_by, request, started_at, error, result, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetOperation :one
SELECT * FROM operations WHERE id = $1;

-- name: ListOperations :many
SELECT * FROM operations ORDER BY requested_at DESC LIMIT $1;

-- name: RunningJob :one
SELECT * FROM operations
WHERE kind IN ('backup', 'restore') AND status IN ('pending', 'running')
LIMIT 1;

-- name: StartOperation :exec
UPDATE operations SET status = 'running', started_at = NOW(), heartbeat_at = NOW() WHERE id = $1;

-- name: ReportOperationProgress :one
-- The heartbeat and the cancellation check in one round trip.
UPDATE operations SET progress = $2, heartbeat_at = NOW()
WHERE id = $1
RETURNING cancel_requested;

-- name: FinishOperation :exec
UPDATE operations
SET status = $2, result = $3, error = $4, finished_at = NOW(), heartbeat_at = NOW()
WHERE id = $1;

-- name: RequestOperationCancel :execrows
UPDATE operations SET cancel_requested = TRUE
WHERE id = $1 AND status IN ('pending', 'running');

-- name: FailStaleOperations :many
-- Jobs whose node stopped saying it was working on them. Whoever notices
-- first marks them, so a node that died does not hold the lock for ever.
UPDATE operations
SET status = 'failed', finished_at = NOW(),
    error = 'the node running this stopped responding'
WHERE kind IN ('backup', 'restore') AND status IN ('pending', 'running')
  AND heartbeat_at < NOW() - make_interval(secs => sqlc.arg(after_seconds)::int)
RETURNING *;

-- name: PendingOperationsFor :many
-- Reloads and restarts another node asked of this one.
SELECT * FROM operations
WHERE status = 'pending' AND node = $1 AND kind IN ('reload', 'restart')
ORDER BY requested_at;

-- name: ClaimOperation :execrows
UPDATE operations SET status = 'running', started_at = NOW(), heartbeat_at = NOW()
WHERE id = $1 AND status = 'pending';

-- name: RestartsAwaitingReturn :many
-- Restarts of this host that a new process has not yet reported back from.
SELECT * FROM operations
WHERE kind = 'restart' AND status = 'running' AND host = $1
ORDER BY requested_at;

-- name: TouchNode :exec
INSERT INTO server_nodes (id, host, version, started_at, last_seen, connections)
VALUES ($1, $2, $3, $4, NOW(), $5)
ON CONFLICT (id) DO UPDATE SET last_seen = NOW(), connections = EXCLUDED.connections;

-- name: ListNodes :many
SELECT * FROM server_nodes ORDER BY started_at;

-- name: GetNode :one
SELECT * FROM server_nodes WHERE id = $1;

-- name: ForgetNodes :exec
DELETE FROM server_nodes WHERE last_seen < NOW() - make_interval(secs => sqlc.arg(after_seconds)::int) OR id = sqlc.arg(id);

-- name: GetMaintenance :one
SELECT * FROM maintenance WHERE id;

-- name: SetMaintenance :exec
UPDATE maintenance
SET active = $1, reason = $2, operation_id = $3, restart_required = $4, since = NOW()
WHERE id;

-- name: CountOpenCalls :one
SELECT COUNT(*) FROM calls WHERE ended_at IS NULL;
