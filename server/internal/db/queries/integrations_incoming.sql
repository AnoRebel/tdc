-- name: CreateIncomingWebhook :one
INSERT INTO incoming_webhooks (id, room_id, label, token_hash, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetActiveIncomingWebhookByHash :one
-- The one lookup a post makes. No cache in front of it, so a revocation is
-- final for the very next call.
SELECT * FROM incoming_webhooks WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: ListIncomingWebhooksForRoom :many
SELECT * FROM incoming_webhooks WHERE room_id = $1 ORDER BY created_at DESC;

-- name: RevokeIncomingWebhook :one
UPDATE incoming_webhooks
SET revoked_at = now(), revoked_by = $3
WHERE id = $1 AND room_id = $2 AND revoked_at IS NULL
RETURNING *;

-- name: RecordIncomingWebhookPost :exec
UPDATE incoming_webhooks
SET posted_count = posted_count + 1, last_used_at = now()
WHERE id = $1;

-- name: RecordIncomingWebhookRefused :exec
UPDATE incoming_webhooks
SET refused_count = refused_count + 1, last_refused_at = now()
WHERE id = $1;
