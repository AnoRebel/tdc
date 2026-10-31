-- name: SetRoomFederated :exec
-- Opts a room in or out of federation. Explicit, never implied by a server
-- setting: enabling federation must not silently expose existing rooms.
UPDATE rooms SET federated = $2, updated_at = NOW() WHERE id = $1;

-- name: AddRoomPeer :exec
-- Adds a peer to a room. Adding one twice is the same peer: its cursor is left
-- alone rather than reset to zero, which would resend the room's whole history,
-- but its address is refreshed so an operator can correct one in place.
INSERT INTO room_peers (room_id, peer, address)
VALUES ($1, $2, $3)
ON CONFLICT (room_id, peer) DO UPDATE SET address = EXCLUDED.address;

-- name: RemoveRoomPeer :exec
DELETE FROM room_peers WHERE room_id = $1 AND peer = $2;

-- name: RoomPeers :many
-- The peers participating in a room, for delivery and for showing an operator
-- who can see it.
SELECT * FROM room_peers WHERE room_id = $1 ORDER BY peer;

-- name: RoomPeer :one
-- One peer's participation, which is also the check for whether a peer may
-- read a room at all.
SELECT * FROM room_peers WHERE room_id = $1 AND peer = $2;

-- name: AdvanceRoomPeerCursor :exec
-- Records how far a peer has been told.
--
-- Never moves backwards: a late acknowledgement of an earlier batch must not
-- undo a later one, which would resend events the peer already holds.
UPDATE room_peers
SET cursor = GREATEST(cursor, $3),
    delivered_at = NOW(),
    attempted_at = NOW(),
    last_error = ''
WHERE room_id = $1 AND peer = $2;

-- name: EventsForPeerSince :many
/*
A room's events after a cursor, for a peer that participates in it.

The participation check is part of the query rather than something a caller
does first: a peer asking for a room it does not participate in must receive
nothing, and making that a join means there is no path where somebody forgets
to check.

Redacted events are included with their content already emptied by redaction —
the row stays so ordering and verification hold, which is the same thing local
readers see.
*/
SELECT e.* FROM events e
JOIN room_peers rp ON rp.room_id = e.room_id
WHERE e.room_id = $1
  AND rp.peer = $2
  AND e.sequence > $3
ORDER BY e.sequence
LIMIT $4;

-- name: StoreForeignEvent :many
/*
Stores an event that arrived from another server, keeping its sequence.

The sequence is the home server's, never reassigned here: renumbering is the
first step toward two servers deciding a room's order independently, which is
the failure the protocol exists to prevent.

Idempotent on the event's own content-derived identifier. A redelivery — a
retry, an overlapping catch-up, a peer sending a batch twice — writes nothing
and returns no row, so "do not produce duplicates" is a database rule rather
than a check the caller has to remember. The row returned is what is new, for
showing to the people here who are watching.
*/
INSERT INTO events (
    id, room_id, sequence, sender_id, sender_address, room_address,
    origin_server, type, content, content_hash, created_at, nonce, signature, key_id
)
VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: FederatedRoomByAddress :one
-- Finds a federated room by the address an event names, which is how an
-- incoming event says which room it belongs to.
SELECT * FROM rooms WHERE slug_folded = $1 AND federated = TRUE;

-- name: FederatedRoomByID :one
-- A federated room with no slug — a project, an issue's discussion — whose
-- address names its identifier instead.
SELECT * FROM rooms WHERE id = $1 AND slug IS NULL AND federated = TRUE;

-- name: HighestSequenceInRoom :one
-- The furthest this server has stored for a room, which is what a catch-up
-- request asks from.
SELECT COALESCE(MAX(sequence), 0)::bigint FROM events WHERE room_id = $1;

-- name: AdoptForeignRoom :one
/*
Creates the local replica of a room another server owns.

home_server names that other server, which is what makes every later check work
— accepting its events, refusing to assign sequences here, and refusing events
about this room from anybody else. A replica created through the ordinary path
would name this server as home and then correctly refuse everything the real
home server sent it.

Already federated on creation: a replica exists only because somebody federated
the room, so a replica that was not federated would be a room nobody could fill.
*/
INSERT INTO rooms (
    id, slug, slug_folded, name, topic, kind, visibility,
    home_server, created_by, federated
)
VALUES ($1, $2, $3, $4, '', 'channel', $5, $6, NULL, TRUE)
RETURNING *;

-- name: AddRemoteMember :exec
-- Records that somebody on another server is in a room. Idempotent: joining
-- twice is being in the room once, and the role is refreshed so a promotion
-- the home server decided takes effect here.
INSERT INTO remote_members (room_id, address, home_server, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (room_id, address) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveRemoteMember :exec
DELETE FROM remote_members WHERE room_id = $1 AND address = $2;

-- name: RemoteMembers :many
SELECT * FROM remote_members WHERE room_id = $1 ORDER BY address;

-- name: IsRemoteMember :one
SELECT EXISTS (
    SELECT 1 FROM remote_members WHERE room_id = $1 AND address = $2
)::boolean;

-- name: RemoteMember :one
SELECT * FROM remote_members WHERE room_id = $1 AND address = $2;

-- name: RecordDeliveryFailure :one
/*
Counts a failure and says when to try again, abandoning past the ceiling.

Returns the row so the caller can tell an operator what happened without a
second query — and so "this was the attempt that gave up" is knowable at the
moment it happens rather than inferred later.
*/
UPDATE room_peers
SET failures = failures + 1,
    attempted_at = NOW(),
    last_error = $3,
    next_attempt_at = NOW() + sqlc.arg(retry_after)::interval,
    abandoned = (failures + 1) >= sqlc.arg(ceiling)::int
WHERE room_id = $1 AND peer = $2
RETURNING *;

-- name: ClearDeliveryFailures :exec
-- A success resets everything: consecutive failures are what matter, and a
-- peer that answers has none.
UPDATE room_peers
SET failures = 0, next_attempt_at = NULL, abandoned = FALSE, last_error = ''
WHERE room_id = $1 AND peer = $2;

-- name: ResumePeer :exec
-- An operator's decision to try an abandoned peer again.
UPDATE room_peers
SET abandoned = FALSE, failures = 0, next_attempt_at = NULL, last_error = ''
WHERE room_id = $1 AND peer = $2;

-- name: ClaimPeersDue :many
/*
Peers owed events, claimed for one delivery pass.

Only for federated rooms this server is home to: a replica holds a peer row for
the home server it receives from, and must never push that server's own events
back to it. Abandoned peers are excluded here rather than checked by the
caller: a peer past the ceiling must cost nothing.

Claiming moves the next attempt a minute on, under row locks other passes
skip, so two nodes sharing the database never deliver the same batch at once.
A success clears it; a failure replaces it with the backoff.
*/
UPDATE room_peers
SET next_attempt_at = NOW() + interval '1 minute'
WHERE (room_id, peer) IN (
    SELECT rp.room_id, rp.peer FROM room_peers rp
    JOIN rooms r ON r.id = rp.room_id
    WHERE rp.abandoned = FALSE
      AND r.federated
      AND r.home_server = sqlc.arg(home)
      AND (rp.next_attempt_at IS NULL OR rp.next_attempt_at <= NOW())
      AND EXISTS (
          SELECT 1 FROM events e
          WHERE e.room_id = rp.room_id AND e.sequence > rp.cursor
      )
    ORDER BY rp.room_id, rp.peer
    LIMIT sqlc.arg(max)
    FOR UPDATE OF rp SKIP LOCKED
)
RETURNING *;

-- name: PublishedRooms :many
/*
The rooms this server offers to a peer browsing it.

Public rooms only, and never archived ones. A private room is excluded here
rather than filtered by the caller, so "a private room on a public server
remains undiscoverable" is a property of the query rather than of whoever
remembers to check — the spec's requirement, made structural.

Direct conversations are excluded by kind: they have no slug and are found by
their participants, so listing them would disclose who talks to whom.
*/
SELECT id, slug, name, topic, home_server, federated
FROM rooms
WHERE visibility = 'public'
  AND archived_at IS NULL
  AND kind = 'channel'
  AND slug IS NOT NULL
ORDER BY name
LIMIT $1;

-- name: KnownPeers :many
/*
Every server this one participates with, once each.

Peers are recorded per room, so a server sharing four rooms appears four times;
this collapses that to the server itself, which is what "who are we connected
to" actually asks. The counts come along because an operator looking at a peer
wants to know how much is riding on it.
*/
SELECT peer,
       MIN(address)::text AS address,
       COUNT(*)::bigint AS rooms,
       BOOL_OR(abandoned)::boolean AS any_abandoned
FROM room_peers
GROUP BY peer
ORDER BY peer;
