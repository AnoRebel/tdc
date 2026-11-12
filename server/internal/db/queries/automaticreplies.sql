-- name: ListAutomaticReplies :many
SELECT * FROM automatic_replies WHERE user_id = $1 ORDER BY created_at, id;

-- name: CountAutomaticReplies :one
SELECT COUNT(*) FROM automatic_replies WHERE user_id = $1;

-- name: CreateAutomaticReply :one
INSERT INTO automatic_replies (
    id, user_id, enabled, message, mention_message, statuses, starts_at, ends_at,
    time_zone, reply_to_direct, reply_to_groups, reply_to_mentions
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: UpdateAutomaticReply :one
UPDATE automatic_replies
SET enabled = $3, message = $4, mention_message = $5, statuses = $6,
    starts_at = $7, ends_at = $8, time_zone = $9, reply_to_direct = $10,
    reply_to_groups = $11, reply_to_mentions = $12, updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteAutomaticReply :execrows
DELETE FROM automatic_replies WHERE id = $1 AND user_id = $2;

-- name: CountRecentAutomaticReplies :one
-- How many conversations somebody's automatic reply has answered lately, for
-- the hourly ceiling.
SELECT COUNT(*) FROM automatic_reply_log
WHERE user_id = $1 AND sent_at > NOW() - make_interval(secs => sqlc.arg(window_seconds)::float8);

-- name: ClaimAutomaticReply :one
-- Claims the right to answer one sender in one conversation. Returns a row
-- only when nothing was sent to them there within the quiet period, so two
-- messages arriving together cannot both be answered.
INSERT INTO automatic_reply_log (user_id, room_id, sender_id, sent_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (user_id, room_id, sender_id) DO UPDATE
SET sent_at = NOW()
WHERE automatic_reply_log.sent_at <= NOW() - make_interval(secs => sqlc.arg(quiet_seconds)::float8)
RETURNING sent_at;

-- name: ReleaseAutomaticReply :exec
-- Gives back a claim whose reply could not be posted.
DELETE FROM automatic_reply_log WHERE user_id = $1 AND room_id = $2 AND sender_id = $3;
