-- name: CreateScheduledMessage :one
INSERT INTO scheduled_messages (
    id, room_id, sender_id, sender_address, body, attachment_ids,
    thread_root_id, send_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ClaimDueScheduledMessages :many
-- What is ready to send, claimed by whoever asked.
--
-- One statement, so finding a row and claiming it cannot be separated: SKIP
-- LOCKED keeps a concurrent pass off the rows this one is claiming, and the
-- claim keeps every later pass off them until it expires. Five minutes is far
-- longer than sending a batch takes, and short enough that a message held by a
-- server that stopped goes out soon after.
UPDATE scheduled_messages
SET claimed_until = now() + interval '5 minutes'
WHERE id IN (
    SELECT id FROM scheduled_messages
    WHERE send_at <= now() AND sent_event_id IS NULL AND failed_reason = ''
      AND (claimed_until IS NULL OR claimed_until < now())
    ORDER BY send_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkScheduledMessageSent :exec
UPDATE scheduled_messages SET sent_event_id = $2 WHERE id = $1;

-- name: MarkScheduledMessageFailed :exec
UPDATE scheduled_messages SET failed_reason = $2 WHERE id = $1;

-- name: ListScheduledMessagesForUser :many
-- Somebody's own outstanding messages, soonest first.
SELECT * FROM scheduled_messages
WHERE sender_id = $1 AND sent_event_id IS NULL AND failed_reason = ''
ORDER BY send_at;

-- name: GetScheduledMessage :one
SELECT * FROM scheduled_messages WHERE id = $1;

-- name: DeleteScheduledMessage :execrows
-- Scoped to the sender, and only while unsent: cancelling is the sender's to
-- do, and a message already sent is a message, not a plan.
--
-- Nor while a delivery pass holds it. A claimed message is being sent at this
-- moment, and deleting it underneath would tell its author it was cancelled
-- when it is about to arrive.
DELETE FROM scheduled_messages
WHERE id = $1 AND sender_id = $2 AND sent_event_id IS NULL
  AND (claimed_until IS NULL OR claimed_until < now());

-- name: ChangeScheduledMessage :one
-- Rewording or moving a message that has not gone, on the same terms as
-- cancelling it: the sender's, unsent, not failed, and not being sent. Either
-- may be left out to keep it as it was.
UPDATE scheduled_messages
SET body    = COALESCE(sqlc.narg(body), body),
    send_at = COALESCE(sqlc.narg(send_at), send_at)
WHERE id = $1 AND sender_id = $2
  AND sent_event_id IS NULL AND failed_reason = ''
  AND (claimed_until IS NULL OR claimed_until < now())
RETURNING *;

-- name: ClaimScheduledMessage :one
-- One message, claimed to be sent now at its author's request. The same claim
-- a delivery pass takes, so the pass cannot send it a second time.
UPDATE scheduled_messages
SET claimed_until = now() + interval '5 minutes'
WHERE id = $1 AND sender_id = $2
  AND sent_event_id IS NULL AND failed_reason = ''
  AND (claimed_until IS NULL OR claimed_until < now())
RETURNING *;
