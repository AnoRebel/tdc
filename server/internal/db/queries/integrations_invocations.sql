-- name: SetApplicationInvocation :one
-- Only an approved application has anywhere to be told about anything.
UPDATE applications
SET invocation_url = $2, invocation_secret = $3
WHERE id = $1 AND state = 'approved'
RETURNING *;

-- name: EnqueueInvocation :one
INSERT INTO webhook_deliveries (id, target, application_id, event_id, kind, payload)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListApplicationInvocations :many
-- An application's recent invocations, newest first, with how the last
-- attempt at each went.
SELECT d.id, d.event_id, d.kind, d.state, d.attempts, d.created_at, d.completed_at,
       d.next_attempt_at,
       last.status_code AS last_status, last.error AS last_error
FROM webhook_deliveries AS d
LEFT JOIN LATERAL (
    SELECT a.status_code, a.error FROM webhook_attempts AS a
    WHERE a.delivery_id = d.id
    ORDER BY a.attempted_at DESC
    LIMIT 1
) AS last ON true
WHERE d.application_id = $1
ORDER BY d.created_at DESC
LIMIT $2;
