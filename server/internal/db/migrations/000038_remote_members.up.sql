/*
People who are in a room but not on this server.

room_members requires a local users row, which a remote participant has none
of and should not: creating a shadow account for everybody who joins from
elsewhere would mean this server holding records of people it knows nothing
about, who can never sign in here, and who would appear in every place a user
list is drawn.

So remote membership is its own table, keyed by the address rather than by an
identifier. The address is what an event carries and what the home server
decides about, which makes it the thing membership is actually about.

Only the room's home server writes here with authority. A replica keeps a copy
so it can render a member list and check, before forwarding, whether somebody
is even plausibly a member — but the home server's answer is the one that
counts, and a replica's copy going stale is a known limit rather than a bug.
*/
CREATE TABLE remote_members (
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,

    -- user@server, as events carry it.
    address    TEXT        NOT NULL,

    /*
      The server the member is on, derived from the address.

      Stored rather than parsed on every query: removing every member of a
      server that has been blocked or has gone away is then one statement
      rather than a scan with string splitting.
    */
    home_server TEXT       NOT NULL,

    -- What they may do, as the room's home server decided.
    role       TEXT        NOT NULL DEFAULT 'member',

    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (room_id, address),

    CONSTRAINT remote_members_role_valid
        CHECK (role IN ('member', 'moderator'))
);

CREATE INDEX remote_members_server_idx ON remote_members (home_server);

/*
How many times delivery to a peer has failed in a row, and when to try again.

Consecutive failures, reset on any success: a peer that fails once an hour is
not the same as one that has been down for an hour, and counting total failures
would eventually abandon a perfectly healthy peer.
*/
ALTER TABLE room_peers ADD COLUMN failures INTEGER NOT NULL DEFAULT 0;

/*
When the next attempt is due, and whether there will be one.

next_attempt_at null means "as soon as anything is delivered". abandoned marks
a peer past the retry ceiling: delivery stops, the operator is told, and
nothing resumes until they say so — which is the spec's own "permanently
unreachable peer is abandoned" rather than retrying into the void forever.
*/
ALTER TABLE room_peers ADD COLUMN next_attempt_at TIMESTAMPTZ;
ALTER TABLE room_peers ADD COLUMN abandoned BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE room_peers ADD CONSTRAINT room_peers_failures_not_negative
    CHECK (failures >= 0);
