-- Rooms, membership and the event log.

-- A channel or direct conversation.
--
-- Called a room rather than a channel because the same structure carries
-- direct messages and disposable chats; "channel" is one kind of room.
CREATE TABLE rooms (
    id            UUID        PRIMARY KEY,
    -- The local part of the room's address. With the server's own name it
    -- forms room@server, which stays unambiguous once servers federate.
    slug          TEXT        NOT NULL,
    slug_folded   TEXT        NOT NULL UNIQUE,
    name          TEXT        NOT NULL,
    topic         TEXT        NOT NULL DEFAULT '',
    -- 'channel', 'direct' or 'disposable'.
    kind          TEXT        NOT NULL DEFAULT 'channel',
    -- 'public' means any member of this server may discover and join;
    -- 'private' means only invited members may see it at all.
    visibility    TEXT        NOT NULL DEFAULT 'public',
    -- The server that owns this room and is authoritative for its ordering,
    -- membership and moderation. Local rooms carry this server's name.
    home_server   TEXT        NOT NULL,
    created_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Archiving stops new messages while preserving history, and is
    -- reversible.
    archived_at   TIMESTAMPTZ,
    archived_by   UUID        REFERENCES users(id) ON DELETE SET NULL,
    -- For disposable rooms: how long a message survives.
    message_ttl_seconds INTEGER,

    CONSTRAINT rooms_kind_valid CHECK (kind IN ('channel', 'direct', 'disposable')),
    CONSTRAINT rooms_visibility_valid CHECK (visibility IN ('public', 'private')),
    CONSTRAINT rooms_slug_not_empty CHECK (length(trim(slug)) > 0)
);

CREATE INDEX rooms_visibility_idx ON rooms (visibility) WHERE archived_at IS NULL;

-- Who is in a room.
CREATE TABLE room_members (
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    invited_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    -- The last event this member has read, for unread counts. Nullable
    -- because a new member has read nothing.
    read_sequence BIGINT,

    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX room_members_user_id_idx ON room_members (user_id);

-- The append-only event log.
--
-- Every message and state change is a row here. Timelines and membership are
-- derived from it rather than maintained independently, which is what makes
-- them reconstructible.
CREATE TABLE events (
    -- Content-derived, so altering any signed field makes this wrong.
    id           TEXT        PRIMARY KEY,
    room_id      UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    -- Position in the room's total order, assigned on acceptance. Unique per
    -- room, so two events cannot claim the same position.
    sequence     BIGINT      NOT NULL,
    sender_id    UUID        REFERENCES users(id) ON DELETE SET NULL,
    -- The addresses are stored alongside the identifiers so that an event
    -- stays meaningful after an account is deleted, and so a federated event
    -- from a server we no longer know still reads correctly.
    sender_address TEXT      NOT NULL,
    room_address   TEXT      NOT NULL,
    origin_server  TEXT      NOT NULL,
    type         TEXT        NOT NULL,
    content      JSONB       NOT NULL,
    created_at   BIGINT      NOT NULL,
    signature    TEXT        NOT NULL,
    key_id       TEXT        NOT NULL,
    -- Redaction removes content while leaving the event in place, so ordering
    -- and the surrounding events stay verifiable.
    redacted_at  TIMESTAMPTZ,
    redacted_by  UUID        REFERENCES users(id) ON DELETE SET NULL,

    CONSTRAINT events_sequence_positive CHECK (sequence > 0)
);

CREATE UNIQUE INDEX events_room_sequence_idx ON events (room_id, sequence);
CREATE INDEX events_room_created_idx ON events (room_id, sequence DESC);
CREATE INDEX events_sender_idx ON events (sender_id);

-- Allocates sequence numbers per room.
--
-- A separate table rather than MAX(sequence)+1 on insert: the maximum is a
-- read that two concurrent writers can both see, producing the same number.
-- A row that is locked and incremented gives a total order under concurrency.
CREATE TABLE room_sequences (
    room_id  UUID   PRIMARY KEY REFERENCES rooms(id) ON DELETE CASCADE,
    next_seq BIGINT NOT NULL DEFAULT 1
);

-- Records which client submissions have already been accepted.
--
-- A client that retries a submission whose outcome it did not observe must not
-- create a second event. The token is chosen by the client and scoped to the
-- sender, so one client cannot suppress another's message by guessing.
CREATE TABLE event_idempotency (
    sender_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_token TEXT        NOT NULL,
    event_id     TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (sender_id, client_token)
);

CREATE INDEX event_idempotency_created_at_idx ON event_idempotency (created_at);
