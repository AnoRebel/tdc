-- name: CreateWebhookSubscription :one
INSERT INTO webhook_subscriptions (id, owner_id, room_id, label, url, secret, event_types, payload_version, scope)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetWebhookSubscription :one
SELECT * FROM webhook_subscriptions WHERE id = $1;

-- name: ListWebhookSubscriptionsForOwner :many
SELECT * FROM webhook_subscriptions WHERE owner_id = $1 ORDER BY created_at DESC;

-- name: ListActiveWebhookSubscriptionsForRoom :many
-- What an event in this room might need delivering to.
SELECT * FROM webhook_subscriptions WHERE room_id = $1 AND state = 'active';

-- name: ListActiveServerWebhookSubscriptions :many
-- Server-wide subscriptions that asked for this type, wherever it happened.
SELECT * FROM webhook_subscriptions
WHERE scope = 'server' AND state = 'active' AND sqlc.arg(event_type)::text = ANY(event_types);

-- name: DeleteWebhookSubscription :one
-- Scoped to its owner, so a subscription cannot be removed by guessing.
DELETE FROM webhook_subscriptions WHERE id = $1 AND owner_id = $2 RETURNING *;

-- name: DisableWebhookSubscription :one
-- Only an active one, so the owner is told once rather than at every failure
-- that follows.
UPDATE webhook_subscriptions
SET state = 'disabled', disabled_at = now(), disabled_reason = $2, updated_at = now()
WHERE id = $1 AND state = 'active'
RETURNING *;

-- name: EnableWebhookSubscription :one
-- Turning it back on starts the count again; what failed before is history.
UPDATE webhook_subscriptions
SET state = 'active', disabled_at = NULL, disabled_reason = NULL,
    consecutive_failures = 0, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: RecordWebhookRateLimited :exec
UPDATE webhook_subscriptions
SET rate_limited_count = rate_limited_count + 1, last_rate_limited_at = now(), updated_at = now()
WHERE id = $1;

-- name: RecordWebhookAccessLost :exec
UPDATE webhook_subscriptions SET access_lost_at = now(), updated_at = now() WHERE id = $1;

-- name: EnqueueWebhookDelivery :one
INSERT INTO webhook_deliveries (id, target, subscription_id, event_id, kind, payload)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ClaimWebhookDeliveries :many
/*
Up to capacity due deliveries, at most one per target, none to a target that
already has one in flight — oldest first.

One statement for a whole batch, because every claim is a commit, and a commit
is what a busy database makes a writer wait for: claiming one row at a time put
the whole pool in a queue behind itself. The in-flight rule is what keeps a
slow receiver to one worker: its next delivery is not claimed until the current
one is answered, so however slow it is it holds one worker and never the pool —
and its deliveries go out in the order they were queued.

The outer state condition is what makes this safe on several nodes. Two claims
racing for one row both pick it, and the second's update re-reads the row,
finds it no longer pending, and leaves it alone.
*/
UPDATE webhook_deliveries AS d
SET state = 'delivering', claimed_at = now(), attempts = d.attempts + 1
WHERE d.state = 'pending'
  AND d.id IN (
    SELECT firsts.id
    FROM (
        SELECT DISTINCT ON (candidate.target)
               candidate.id, candidate.next_attempt_at, candidate.created_at
        FROM webhook_deliveries AS candidate
        WHERE candidate.state = 'pending'
          AND candidate.next_attempt_at <= now()
          AND candidate.target <> ALL (ARRAY(
              SELECT busy.target FROM webhook_deliveries AS busy WHERE busy.state = 'delivering'))
        ORDER BY candidate.target, candidate.next_attempt_at, candidate.created_at
    ) AS firsts
    ORDER BY firsts.next_attempt_at, firsts.created_at
    LIMIT sqlc.arg(capacity)
  )
RETURNING d.*;

-- name: FinishWebhookAttempt :exec
/*
Everything an attempt's outcome changes, in one statement and so one commit:
the attempt recorded, the delivery moved on, and the subscription's run of
failures counted or reset.

Separate writes cost a commit each, four per delivery, and under load that
alone decided how many deliveries a second the pool could make.
*/
WITH recorded AS (
    INSERT INTO webhook_attempts (id, delivery_id, attempt, outcome, status_code, error, response, duration_ms)
    VALUES (sqlc.arg(attempt_id), sqlc.arg(delivery_id), sqlc.arg(attempt), sqlc.arg(outcome),
            sqlc.narg(status_code), sqlc.narg(error), sqlc.narg(response), sqlc.arg(duration_ms))
), moved AS (
    UPDATE webhook_deliveries
    SET state = sqlc.arg(state)::text,
        claimed_at = NULL,
        next_attempt_at = COALESCE(sqlc.narg(next_attempt_at)::timestamptz, next_attempt_at),
        completed_at = CASE WHEN sqlc.arg(state)::text = 'pending' THEN NULL ELSE now() END
    WHERE id = sqlc.arg(delivery_id)
)
UPDATE webhook_subscriptions
SET consecutive_failures = CASE WHEN sqlc.arg(delivered)::boolean THEN 0 ELSE consecutive_failures + 1 END,
    access_lost_at = CASE WHEN sqlc.arg(delivered)::boolean THEN NULL ELSE access_lost_at END,
    updated_at = now()
WHERE id = sqlc.narg(subscription_id)::uuid;

-- name: CompleteWebhookDelivery :exec
UPDATE webhook_deliveries
SET state = $2, completed_at = now(), claimed_at = NULL
WHERE id = $1;

-- name: PostponeWebhookDelivery :exec
-- Held back by a rate limit: not an attempt, so the count is given back.
UPDATE webhook_deliveries
SET state = 'pending', next_attempt_at = $2, claimed_at = NULL, attempts = attempts - 1
WHERE id = $1;

-- name: ReleaseStaleWebhookDeliveries :execrows
-- Deliveries a worker claimed and never finished, because the process ended
-- mid-attempt. Put back rather than lost; the receiver may see one twice,
-- which the delivery identifier lets it notice.
UPDATE webhook_deliveries
SET state = 'pending', claimed_at = NULL
WHERE state = 'delivering' AND claimed_at < $1;

-- name: CancelPendingWebhookDeliveries :execrows
-- Everything still waiting for a subscription that has been turned off.
UPDATE webhook_deliveries
SET state = 'cancelled', completed_at = now()
WHERE subscription_id = $1 AND state = 'pending';

-- name: RecordWebhookAttempt :exec
INSERT INTO webhook_attempts (id, delivery_id, attempt, outcome, status_code, error, response, duration_ms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListWebhookHistory :many
-- A subscription's recent attempts, newest first, with what each was for.
SELECT a.id, a.delivery_id, a.attempt, a.attempted_at, a.outcome, a.status_code,
       a.error, a.response, a.duration_ms, d.kind, d.event_id, d.state AS delivery_state
FROM webhook_attempts AS a
JOIN webhook_deliveries AS d ON d.id = a.delivery_id
WHERE d.subscription_id = $1
ORDER BY a.attempted_at DESC
LIMIT $2;

-- name: ListWebhookDeliveries :many
-- A subscription's recent deliveries, including ones not yet attempted.
SELECT * FROM webhook_deliveries
WHERE subscription_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: PruneWebhookDeliveries :execrows
-- History older than the retention period. Only finished deliveries: one still
-- waiting is not history yet, however old.
DELETE FROM webhook_deliveries
WHERE created_at < $1 AND state IN ('delivered', 'abandoned', 'cancelled');
