-- name: CreateRoom :one
INSERT INTO rooms (id, slug, slug_folded, name, topic, kind, visibility, home_server, created_by, message_ttl_seconds)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetRoomByID :one
SELECT * FROM rooms WHERE id = $1;

-- name: GetRoomBySlug :one
SELECT * FROM rooms WHERE slug_folded = $1;

-- name: ListRoomsForUser :many
-- Rooms the user is a member of, plus public rooms they could join. A private
-- room they are not in never appears, which is what keeps its existence
-- undisclosed.
SELECT DISTINCT rooms.*
FROM rooms
LEFT JOIN room_members ON room_members.room_id = rooms.id AND room_members.user_id = $1
WHERE room_members.user_id IS NOT NULL
   OR rooms.visibility = 'public'
ORDER BY rooms.name;

-- name: ListRoomsUserIsIn :many
SELECT rooms.*
FROM rooms
JOIN room_members ON room_members.room_id = rooms.id
WHERE room_members.user_id = $1
ORDER BY rooms.name;

-- name: RenameRoom :one
UPDATE rooms SET name = $2, topic = $3, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: ArchiveRoom :exec
UPDATE rooms SET archived_at = NOW(), archived_by = $2, updated_at = NOW()
WHERE id = $1 AND archived_at IS NULL;

-- name: RestoreRoom :exec
UPDATE rooms SET archived_at = NULL, archived_by = NULL, updated_at = NOW()
WHERE id = $1;

-- name: AddRoomMember :exec
INSERT INTO room_members (room_id, user_id, invited_by)
VALUES ($1, $2, $3)
ON CONFLICT (room_id, user_id) DO NOTHING;

-- name: RemoveRoomMember :exec
DELETE FROM room_members WHERE room_id = $1 AND user_id = $2;

-- name: IsRoomMember :one
SELECT EXISTS (
    SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2
);

-- name: ListRoomMembers :many
SELECT users.id, users.username, users.display_name, room_members.joined_at, room_members.read_sequence
FROM room_members
JOIN users ON users.id = room_members.user_id
WHERE room_members.room_id = $1
ORDER BY users.username;

-- name: ListRoomMemberIDs :many
-- Just the identifiers, for deciding who a live event should reach.
SELECT user_id FROM room_members WHERE room_id = $1;

-- name: SetReadPosition :exec
UPDATE room_members SET read_sequence = GREATEST(COALESCE(read_sequence, 0), $3)
WHERE room_id = $1 AND user_id = $2;
