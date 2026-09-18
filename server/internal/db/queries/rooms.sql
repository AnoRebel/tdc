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

-- name: ListRoomMembersWithDisclosure :many
-- Who is in a channel, with the details each person allows to be disclosed.
--
-- The withholding happens here rather than in Go, and that is the point of the
-- query: a detail somebody has withheld is never read out of the database, so
-- there is no value in memory for a later change to send by accident. A
-- withheld detail comes back as the zero value, and the withholds_* flags
-- beside it are what tell the handler to omit the field rather than send a
-- false or an empty string that would read as an answer.
--
-- user_privacy is sparse — a row appears when somebody first changes a setting
-- — so every flag is read through COALESCE with the disclosing default. A plain
-- join would withhold everything from everybody who has never opened the
-- settings, which is nearly everybody.
--
-- sqlc.arg(as_moderator) is the moderation context. A moderator reviewing a
-- report needs to see what was withheld — a report about somebody's status
-- cannot be judged without it — and the handler records that access in the
-- audit log. Passed as an argument rather than served by a second query so the
-- two cannot drift into disagreeing about what "withheld" means.
SELECT
    users.id,
    users.username,
    users.display_name,
    room_members.joined_at,
    (CASE WHEN COALESCE(user_privacy.shows_presence, TRUE) OR sqlc.arg(as_moderator)::boolean
          -- COALESCE inside, because somebody who has never connected has no
          -- user_presence row: connections is NULL there, and NULL > 0 is NULL
          -- rather than false. Offline is the right answer for them.
          THEN COALESCE(user_presence.connections, 0) > 0
               AND NOT COALESCE(user_presence.invisible, FALSE)
          ELSE FALSE
     END)::boolean AS online,
    (CASE WHEN COALESCE(user_privacy.shows_status, TRUE) OR sqlc.arg(as_moderator)::boolean
          THEN COALESCE(user_presence.status, '')
          ELSE ''
     END)::text AS status,
    (CASE WHEN COALESCE(user_privacy.shows_status, TRUE) OR sqlc.arg(as_moderator)::boolean
          THEN COALESCE(user_presence.status_text, '')
          ELSE ''
     END)::text AS status_text,
    -- The zero timestamp stands for "no answer": the detail was withheld, or
    -- the person has never connected and there is no user_presence row. sqlc
    -- cannot infer a nullable type through a CASE, so rather than leave this
    -- untyped it is cast, and the handler omits the field when it is zero —
    -- which it must do anyway for the withheld case.
    (CASE WHEN COALESCE(user_privacy.shows_last_seen, TRUE) OR sqlc.arg(as_moderator)::boolean
          THEN COALESCE(user_presence.last_seen_at, 'epoch'::timestamptz)
          ELSE 'epoch'::timestamptz
     END)::timestamptz AS last_seen_at,
    -- What this person withholds. These decide which fields the response omits,
    -- and tell the handler whether a moderator's view disclosed anything worth
    -- recording. Never sent to an ordinary caller.
    (NOT COALESCE(user_privacy.shows_presence, TRUE))::boolean  AS withholds_presence,
    (NOT COALESCE(user_privacy.shows_status, TRUE))::boolean    AS withholds_status,
    (NOT COALESCE(user_privacy.shows_last_seen, TRUE))::boolean AS withholds_last_seen
FROM room_members
JOIN users ON users.id = room_members.user_id
-- Left, because presence is created on first connection: somebody who has
-- never connected has no row, and an inner join would drop them from the
-- member list entirely. The same is true of user_privacy.
LEFT JOIN user_presence ON user_presence.user_id = users.id
LEFT JOIN user_privacy ON user_privacy.user_id = users.id
WHERE room_members.room_id = $1
ORDER BY users.username;

-- name: ListRoomMemberIDs :many
-- Just the identifiers, for deciding who a live event should reach.
SELECT user_id FROM room_members WHERE room_id = $1;

-- name: SetReadPosition :exec
UPDATE room_members SET read_sequence = GREATEST(COALESCE(read_sequence, 0), $3)
WHERE room_id = $1 AND user_id = $2;

-- name: CountRooms :one
-- How many channels exist, for the operator's capacity limit.
--
-- Direct conversations are excluded: they are created by people talking to
-- each other rather than by anybody choosing to add a channel, and counting
-- them would make the limit refuse conversations an operator never meant to
-- cap.
SELECT count(*) FROM rooms WHERE kind <> 'direct';
