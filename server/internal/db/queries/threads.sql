-- name: ListThreadReplies :many
SELECT * FROM events
WHERE thread_root_id = $1
ORDER BY sequence;

-- name: CountThreadReplies :one
SELECT COUNT(*) FROM events WHERE thread_root_id = $1 AND type = 'message.sent';

-- name: ListThreadSummaries :many
-- How many replies each of a set of messages has, and when the latest arrived,
-- so a timeline can show "3 replies" without fetching them.
SELECT thread_root_id, COUNT(*)::bigint AS reply_count, MAX(created_at)::bigint AS last_reply_at
FROM events
WHERE thread_root_id = ANY($1::text[]) AND type = 'message.sent'
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
  -- Replies have their own read position, per thread.
  AND events.thread_root_id IS NULL
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
    AND events.thread_root_id IS NULL
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
--
-- Unread is counted only in threads the reader follows: every thread in a busy
-- channel showing a badge would be a badge nobody reads.
SELECT e.thread_root_id,
       COUNT(*)::bigint AS reply_count,
       MAX(e.created_at)::bigint AS last_reply_at,
       COUNT(*) FILTER (
           WHERE COALESCE(r.following, false)
             AND e.sequence > COALESCE(r.read_sequence, 0)
             AND (e.sender_id IS NULL OR e.sender_id <> @user_id::uuid)
       )::bigint AS unread
FROM events e
LEFT JOIN thread_reads r ON r.root_event_id = e.thread_root_id AND r.user_id = @user_id::uuid
WHERE e.room_id = @room_id::uuid AND e.thread_root_id IS NOT NULL AND e.type = 'message.sent'
GROUP BY e.thread_root_id;

-- name: GetThreadRead :one
SELECT * FROM thread_reads WHERE user_id = $1 AND root_event_id = $2;

-- name: MarkThreadRead :exec
-- Never backwards: a view that is behind cannot resurface replies already read.
INSERT INTO thread_reads (user_id, root_event_id, read_sequence)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, root_event_id)
DO UPDATE SET read_sequence = GREATEST(thread_reads.read_sequence, EXCLUDED.read_sequence);

-- name: ChooseThreadFollowing :exec
-- Somebody deciding for themselves, which participation never overrides.
INSERT INTO thread_reads (user_id, root_event_id, following, chosen)
VALUES ($1, $2, $3, true)
ON CONFLICT (user_id, root_event_id)
DO UPDATE SET following = EXCLUDED.following, chosen = true;

-- name: FollowThreadByTakingPart :exec
-- Following because of taking part, unless the person has already decided.
INSERT INTO thread_reads (user_id, root_event_id, following)
VALUES ($1, $2, true)
ON CONFLICT (user_id, root_event_id)
DO UPDATE SET following = true WHERE NOT thread_reads.chosen;

-- name: ListThreadFollowers :many
SELECT user_id FROM thread_reads WHERE root_event_id = $1 AND following;
