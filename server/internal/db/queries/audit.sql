-- name: AppendAuditEntry :one
INSERT INTO audit_entries (actor_id, actor_label, action, target_kind, target_id, source_ip, user_agent, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListAuditEntries :many
-- Filters are optional: a NULL argument matches everything, so one query
-- serves the whole administrative view rather than one per filter combination.
SELECT * FROM audit_entries
WHERE (sqlc.narg('actor_id')::uuid IS NULL OR actor_id = sqlc.narg('actor_id')::uuid)
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('target_kind')::text IS NULL OR target_kind = sqlc.narg('target_kind')::text)
  AND (sqlc.narg('since')::timestamptz IS NULL OR occurred_at >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR occurred_at <= sqlc.narg('until')::timestamptz)
ORDER BY occurred_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: DeleteAuditEntriesBefore :execrows
DELETE FROM audit_entries WHERE occurred_at < $1;
