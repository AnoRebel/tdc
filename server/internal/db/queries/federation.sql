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

-- name: RecordRoomPeerFailure :exec
-- Records that delivery was tried and did not work, leaving the cursor where
-- it was so the same events are tried again.
UPDATE room_peers
SET attempted_at = NOW(), last_error = $3
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

-- name: StoreForeignEvent :execrows
/*
Stores an event that arrived from another server, keeping its sequence.

The sequence is the home server's, never reassigned here: renumbering is the
first step toward two servers deciding a room's order independently, which is
the failure the protocol exists to prevent.

Idempotent on the event's own content-derived identifier. A redelivery — a
retry, an overlapping catch-up, a peer sending a batch twice — writes nothing
and reports zero rows, so "do not produce duplicates" is a database rule rather
than a check the caller has to remember.
*/
INSERT INTO events (
    id, room_id, sequence, sender_id, sender_address, room_address,
    origin_server, type, content, content_hash, created_at, nonce, signature, key_id
)
VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (id) DO NOTHING;

-- name: FederatedRoomByAddress :one
-- Finds a federated room by the address an event names, which is how an
-- incoming event says which room it belongs to.
SELECT * FROM rooms WHERE slug_folded = $1 AND federated = TRUE;

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
