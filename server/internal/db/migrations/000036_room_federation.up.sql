/*
Which rooms cross between servers, and how far each peer has been told.

A room is not federated because federation is enabled; it is federated because
somebody said so about that room. The two are deliberately separate — enabling
federation on a server should not silently expose every conversation it holds —
so this is an explicit per-room opt-in rather than a consequence of a server
setting.

The cursor is the whole of the delivery machinery's state. A peer has seen
everything up to some sequence in a room and nothing after it, so "what does
this peer not have yet" is one comparison rather than a queue to maintain, and
a peer that was unreachable for a day asks for everything after its cursor
instead of receiving a day of individually retried pushes.
*/

-- Whether this room is exposed to peers at all. False everywhere on migration,
-- which is the answer for every room that existed before anybody could choose.
ALTER TABLE rooms ADD COLUMN federated BOOLEAN NOT NULL DEFAULT FALSE;

/*
The peers participating in a room, one row each.

Rows exist only for rooms somebody federated, so this table is empty on a
server that federates nothing — the cost of the feature is not carried by
servers that do not use it.
*/
CREATE TABLE room_peers (
    room_id     UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,

    /*
      The peer, as a host.

      Stored the way the trust lists compare them — a hostname without a port,
      since a server on :8443 is the same server as on :443. Storing the full
      address here would mean the same peer written two ways became two rows.
    */
    peer        TEXT        NOT NULL,

    /*
      The last sequence this peer has acknowledged for this room.

      Zero means it has been told nothing yet, which is the honest starting
      point: a peer that has just been added has seen none of the room's
      history, and the first delivery gives it everything.
    */
    cursor      BIGINT      NOT NULL DEFAULT 0,

    /*
      When delivery to this peer last succeeded, and when it was last tried.

      Both, because they answer different questions. An operator asking "is
      this peer keeping up" needs the first; one asking "is anything still
      trying" needs the second, and a peer that has been failing for an hour
      looks identical to a healthy one if only successes are recorded.
    */
    delivered_at TIMESTAMPTZ,
    attempted_at TIMESTAMPTZ,

    /*
      Why delivery last failed, empty while it is working.

      Kept on the row rather than only in the log so that an operator looking
      at a room's peers sees the reason beside the peer it concerns, rather
      than searching a log for it.
    */
    last_error  TEXT        NOT NULL DEFAULT '',

    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One row per peer per room: adding a peer twice is the same peer, and a
    -- second row would mean two cursors disagreeing about what it has seen.
    PRIMARY KEY (room_id, peer),

    CONSTRAINT room_peers_cursor_not_negative CHECK (cursor >= 0)
);

-- Finding every room a peer participates in, which is what serving that peer's
-- catch-up request asks.
CREATE INDEX room_peers_peer_idx ON room_peers (peer);
