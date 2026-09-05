-- name: CreateScheduledMessage :one
INSERT INTO scheduled_messages (
    id, room_id, sender_id, sender_address, body, attachment_ids,
    thread_root_id, send_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListDueScheduledMessages :many
-- What is ready to send.
--
-- FOR UPDATE SKIP LOCKED so more than one server can run the delivery pass
-- without sending the same message twice: whoever locks a row owns it, and the
-- others move on rather than waiting.
SELECT * FROM scheduled_messages
WHERE send_at <= now() AND sent_event_id IS NULL AND failed_reason = ''
ORDER BY send_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

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
DELETE FROM scheduled_messages
WHERE id = $1 AND sender_id = $2 AND sent_event_id IS NULL;
