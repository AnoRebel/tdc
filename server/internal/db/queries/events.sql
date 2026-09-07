-- name: NextRoomSequence :one
-- Allocates the next position in a room's total order.
--
-- The UPDATE takes a row lock, so two concurrent writers serialise here rather
-- than both reading the same maximum and colliding.
INSERT INTO room_sequences (room_id, next_seq)
VALUES ($1, 2)
ON CONFLICT (room_id) DO UPDATE SET next_seq = room_sequences.next_seq + 1
RETURNING (next_seq - 1)::bigint;

-- name: AppendEvent :one
INSERT INTO events (
    id, room_id, sequence, sender_id, sender_address, room_address,
    origin_server, type, content, created_at, nonce, signature, key_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: GetEventByID :one
SELECT * FROM events WHERE id = $1;

-- name: ListRoomEvents :many
-- A page of a room's timeline, oldest first within the page.
SELECT * FROM events
WHERE room_id = $1 AND sequence > $2 AND sequence <= $3
ORDER BY sequence;

-- name: ListRecentRoomEvents :many
-- The most recent events, for opening a room.
SELECT * FROM events
WHERE room_id = $1
ORDER BY sequence DESC
LIMIT $2;

-- name: LatestRoomSequence :one
SELECT COALESCE(MAX(sequence), 0)::bigint FROM events WHERE room_id = $1;

-- name: CountEventsSince :one
-- How far behind a client is, so the server can decide between replaying the
-- gap and telling the client to resynchronise.
SELECT COUNT(*) FROM events WHERE room_id = $1 AND sequence > $2;

-- name: RedactEvent :exec
-- Removes content while leaving the event in place, so ordering and the
-- surrounding events stay verifiable.
UPDATE events
SET content = '{}'::jsonb, redacted_at = NOW(), redacted_by = $2
WHERE id = $1 AND redacted_at IS NULL;

-- name: RecordIdempotentSubmission :exec
INSERT INTO event_idempotency (sender_id, client_token, event_id)
VALUES ($1, $2, $3)
ON CONFLICT (sender_id, client_token) DO NOTHING;

-- name: FindIdempotentSubmission :one
SELECT event_id FROM event_idempotency
WHERE sender_id = $1 AND client_token = $2;

-- name: DeleteExpiredIdempotencyRecords :exec
DELETE FROM event_idempotency WHERE created_at < $1;

-- name: ExpireEvents :many
-- Redacts messages past their channel's retention period.
--
-- The channel's own period where it has one, and the server's default
-- otherwise: @default_seconds is 0 when the operator has set none, and the
-- comparison then admits nothing, which is how "no policy" means "keep
-- everything" without a separate branch.
--
-- Returns what was expired so the caller can remove the attachments too. A
-- message whose text went while its photographs stayed would be a retention
-- policy that did not retain anything less.
--
-- Only message events. A channel that lost its own creation and membership
-- history would be a channel nobody could audit, and those carry no content
-- worth expiring.
WITH due AS (
    SELECT e.id
    FROM events e
    JOIN rooms r ON r.id = e.room_id
    WHERE e.redacted_at IS NULL
      AND e.type IN ('message.sent', 'message.edited')
      AND COALESCE(r.message_ttl_seconds, @default_seconds::int) > 0
      AND e.created_at < (
          EXTRACT(EPOCH FROM NOW()) * 1000
          - COALESCE(r.message_ttl_seconds, @default_seconds::int)::bigint * 1000
      )
    ORDER BY e.created_at
    LIMIT @batch_size::int
)
UPDATE events
SET content = '{}'::jsonb, redacted_at = NOW()
FROM due
WHERE events.id = due.id
RETURNING events.id, events.room_id;
