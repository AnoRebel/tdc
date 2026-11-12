/*
Calls between people on different servers.

A call is held by one server, its home. Somebody from another server taking
part in a call here is a row in users standing for them: their address, the
server that vouched for them and is sent what the call sends them, and the
identifier their own server knows them by, so their own client recognises
itself in the call. Like a guest of a call it holds no password and no
session, and is in no list of this server's people.
*/
ALTER TABLE users
    ADD COLUMN remote_address TEXT UNIQUE,
    ADD COLUMN remote_server  TEXT;

-- When the server of somebody from elsewhere last said they are still there:
-- nobody here holds their connection, so the sweep goes by this instead.
ALTER TABLE call_participants ADD COLUMN remote_seen_at TIMESTAMPTZ;

-- People on other servers rung into a call here, who may therefore join it.
CREATE TABLE call_remote_invites (
    call_id    UUID        NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    address    TEXT        NOT NULL,
    invited_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    invited_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (call_id, address)
);

/*
Calls held by other servers that people here are rung into or are in. Only the
home server recorded here may send them anything about that call.
*/
CREATE TABLE remote_calls (
    call_id      UUID        NOT NULL,
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The home server's identity (its host), and where to reach it.
    home         TEXT        NOT NULL,
    home_address TEXT        NOT NULL,
    -- This server's copy of the call's room, for a call in a shared room.
    room_id      UUID        REFERENCES rooms(id) ON DELETE CASCADE,
    rung_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    joined_at    TIMESTAMPTZ,
    left_at      TIMESTAMPTZ,
    PRIMARY KEY (call_id, user_id)
);

CREATE INDEX remote_calls_present ON remote_calls (user_id) WHERE joined_at IS NOT NULL AND left_at IS NULL;
