-- name: PinMessage :exec
-- Pinning something already pinned updates who pinned it and when, rather than
-- failing: the second person's intent is the current one.
INSERT INTO pinned_messages (room_id, event_id, pinned_by, pinned_by_address)
VALUES ($1, $2, $3, $4)
ON CONFLICT (room_id, event_id)
DO UPDATE SET pinned_by = EXCLUDED.pinned_by,
              pinned_by_address = EXCLUDED.pinned_by_address,
              pinned_at = now();

-- name: UnpinMessage :exec
DELETE FROM pinned_messages WHERE room_id = $1 AND event_id = $2;

-- name: ListPinnedMessages :many
-- The pinned messages of a room, most recently pinned first, with the message
-- itself so a client does not have to fetch each one.
--
-- sqlc.embed keeps the event as a whole struct rather than flattening its
-- columns alongside the pin's. Flattened, adding a column to events would
-- silently stop it reaching this query's callers.
SELECT sqlc.embed(e), p.pinned_by_address, p.pinned_at
FROM pinned_messages p
JOIN events e ON e.id = p.event_id
WHERE p.room_id = $1
ORDER BY p.pinned_at DESC;

-- name: ListPinnedEventIDs :many
-- Just the identifiers, so a timeline can mark which messages are pinned
-- without fetching them a second time.
SELECT event_id FROM pinned_messages WHERE room_id = $1;

-- name: PinRoom :exec
-- A person's own pinned conversation, which says nothing to anybody else.
INSERT INTO pinned_rooms (user_id, room_id, position)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, room_id) DO UPDATE SET position = EXCLUDED.position;

-- name: UnpinRoom :exec
DELETE FROM pinned_rooms WHERE user_id = $1 AND room_id = $2;

-- name: ListPinnedRooms :many
SELECT room_id, position FROM pinned_rooms
WHERE user_id = $1
ORDER BY position, pinned_at;
