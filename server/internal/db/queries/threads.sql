-- name: AppendThreadedEvent :one
-- A reply carries the identifier of the message that started the thread.
INSERT INTO events (
    id, room_id, sequence, sender_id, sender_address, room_address,
    origin_server, type, content, created_at, nonce, signature, key_id,
    thread_root_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: ListThreadReplies :many
SELECT * FROM events
WHERE thread_root_id = $1
ORDER BY sequence;

-- name: CountThreadReplies :one
SELECT COUNT(*) FROM events WHERE thread_root_id = $1;

-- name: ListThreadSummaries :many
-- How many replies each of a set of messages has, and when the latest arrived,
-- so a timeline can show "3 replies" without fetching them.
SELECT thread_root_id, COUNT(*)::bigint AS reply_count, MAX(created_at)::bigint AS last_reply_at
FROM events
WHERE thread_root_id = ANY($1::text[])
GROUP BY thread_root_id;

-- name: ListRoomTimelineExcludingReplies :many
-- The main timeline: replies live in their thread, not in the channel, unless
-- explicitly surfaced.
SELECT * FROM events
WHERE room_id = $1 AND thread_root_id IS NULL
ORDER BY sequence DESC
LIMIT $2;

-- name: FindDirectRoom :one
SELECT * FROM rooms WHERE participant_key = $1;

-- name: CreateDirectRoom :one
INSERT INTO rooms (id, name, kind, visibility, home_server, created_by, participant_key)
VALUES ($1, $2, 'direct', 'private', $3, $4, $5)
RETURNING *;

-- name: ListDirectRoomsForUser :many
SELECT rooms.*
FROM rooms
JOIN room_members ON room_members.room_id = rooms.id
WHERE room_members.user_id = $1 AND rooms.kind = 'direct'
ORDER BY rooms.updated_at DESC;

-- name: CountUnread :one
-- Messages after the member's read position, excluding their own: a person's
-- own messages are not unread to them.
SELECT COUNT(*)
FROM events
JOIN room_members ON room_members.room_id = events.room_id
WHERE events.room_id = $1
  AND room_members.user_id = $2
  AND events.sequence > COALESCE(room_members.read_sequence, 0)
  AND events.type = 'message.sent'
  AND (events.sender_id IS NULL OR events.sender_id <> $2);

-- name: ListUnreadCounts :many
-- Unread counts for every room a person is in, in one query rather than one
-- per room.
SELECT room_members.room_id,
       COUNT(events.id)::bigint AS unread
FROM room_members
LEFT JOIN events ON events.room_id = room_members.room_id
    AND events.sequence > COALESCE(room_members.read_sequence, 0)
    AND events.type = 'message.sent'
    AND (events.sender_id IS NULL OR events.sender_id <> room_members.user_id)
WHERE room_members.user_id = $1
GROUP BY room_members.room_id;

-- name: ListRoomThreadSummaries :many
-- Reply counts for every thread in a room, in one query.
--
-- By room rather than by a list of message identifiers: a timeline wants the
-- counts for what it is drawing, and asking per message meant one request per
-- message. Replies are counted against the room they were sent to, so a root
-- that has scrolled out of the loaded window still reports correctly.
SELECT thread_root_id,
       COUNT(*)::bigint AS reply_count,
       MAX(created_at)::bigint AS last_reply_at
FROM events
WHERE room_id = $1 AND thread_root_id IS NOT NULL
GROUP BY thread_root_id;
