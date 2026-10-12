/*
Links to a conversation's call.

A link admits to the call only — through call_invites, as merging does — and
never to the conversation. Its secret is stored hashed: it is drawn from the
system's own randomness, so the defence is its entropy, not a slow hash. A
passcode is chosen by a person and so is stored as a password is.
*/
CREATE TABLE call_links (
    id            UUID        PRIMARY KEY,
    room_id       UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    token_hash    BYTEA       NOT NULL UNIQUE,
    label         TEXT        NOT NULL DEFAULT '',
    passcode_hash TEXT,
    -- Whether people wait until somebody in the call admits them.
    lobby         BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Whether people without an account may use it, where the server allows guests.
    guests        BOOLEAN     NOT NULL DEFAULT FALSE,
    expires_at    TIMESTAMPTZ,
    max_uses      INTEGER,
    uses          INTEGER     NOT NULL DEFAULT 0,
    created_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at    TIMESTAMPTZ,
    -- Wrong passcodes in the current window. Past the limit the link refuses
    -- every passcode, the right one too, until the window passes.
    passcode_failures      INTEGER     NOT NULL DEFAULT 0,
    passcode_failures_from TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX call_links_room ON call_links (room_id) WHERE revoked_at IS NULL;

-- People waiting to be let into a call through a link with a lobby.
CREATE TABLE call_lobby (
    call_id      UUID        NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    link_id      UUID        NOT NULL REFERENCES call_links(id) ON DELETE CASCADE,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (call_id, user_id)
);
