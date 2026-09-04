-- Threads, direct messages, presence, status and typing.

-- A message may be the root of a thread, and a reply names its root.
--
-- Stored on the event rather than in a separate table: a reply is an ordinary
-- message that happens to belong to a thread, and keeping it in the same log
-- means the thread inherits the room's ordering and permissions for free.
ALTER TABLE events ADD COLUMN thread_root_id TEXT REFERENCES events(id) ON DELETE SET NULL;

-- Replies are read by thread, so the index leads with the root.
CREATE INDEX events_thread_idx ON events (thread_root_id, sequence)
    WHERE thread_root_id IS NOT NULL;

-- Direct conversations are rooms with no slug of their own.
--
-- A slug would make them addressable and discoverable, which a private
-- conversation must not be. They are found by their participants instead.
ALTER TABLE rooms ALTER COLUMN slug DROP NOT NULL;
ALTER TABLE rooms ALTER COLUMN slug_folded DROP NOT NULL;
ALTER TABLE rooms DROP CONSTRAINT IF EXISTS rooms_slug_not_empty;

-- Identifies a direct conversation by its participants, so asking for the
-- conversation with someone returns the existing one rather than making a
-- second. Built from the sorted participant identifiers, so the order in which
-- two people open it does not matter.
ALTER TABLE rooms ADD COLUMN participant_key TEXT;
CREATE UNIQUE INDEX rooms_participant_key_idx ON rooms (participant_key)
    WHERE participant_key IS NOT NULL;

-- Whether someone is currently connected, and what they have chosen to show.
--
-- Presence is derived from live connections, which are per-process, so this
-- table is what lets several server processes agree. A row that stops being
-- refreshed is treated as offline, which is how an ungraceful disconnect
-- expires without anyone having to notice it.
CREATE TABLE user_presence (
    user_id       UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- How many live connections this person holds. Presence ends when the last
    -- one closes, not the first.
    connections   INTEGER     NOT NULL DEFAULT 0,
    -- The operator-configured status this person has chosen.
    status        TEXT        NOT NULL DEFAULT 'available',
    status_text   TEXT        NOT NULL DEFAULT '',
    -- A status may lapse on its own, so a temporary one needs no action later.
    status_expires_at TIMESTAMPTZ,
    -- Appearing offline while still connected. Stored separately from status
    -- so choosing it does not discard the status underneath.
    invisible     BOOLEAN     NOT NULL DEFAULT FALSE,
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX user_presence_last_seen_idx ON user_presence (last_seen_at)
    WHERE connections > 0;

-- Statuses an operator makes available.
--
-- Configurable rather than fixed, because what a community needs to signal
-- differs: a work server wants "in a meeting", a study group wants "revising".
CREATE TABLE server_statuses (
    id          UUID        PRIMARY KEY,
    key         TEXT        NOT NULL UNIQUE,
    label       TEXT        NOT NULL,
    -- Whether holding this status withholds notifications.
    suppresses_notifications BOOLEAN NOT NULL DEFAULT FALSE,
    -- Presentation only; the server attaches no meaning to it.
    colour      TEXT        NOT NULL DEFAULT '',
    sort_order  INTEGER     NOT NULL DEFAULT 0,
    built_in    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The statuses every server starts with. An operator may add or remove others.
INSERT INTO server_statuses (id, key, label, suppresses_notifications, sort_order, built_in) VALUES
    (gen_random_uuid(), 'available', 'Available',   FALSE, 1, TRUE),
    (gen_random_uuid(), 'busy',      'Busy',        TRUE,  2, TRUE),
    (gen_random_uuid(), 'away',      'Away',        FALSE, 3, TRUE),
    (gen_random_uuid(), 'offline',   'Appear offline', TRUE, 4, TRUE);

-- Who is currently composing a message.
--
-- Rows expire rather than being deleted on stopping: a client that goes away
-- mid-sentence would otherwise leave an indicator on forever.
CREATE TABLE typing_indicators (
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX typing_indicators_expires_idx ON typing_indicators (expires_at);

-- Whether a person sends typing indication at all.
ALTER TABLE users ADD COLUMN sends_typing_indication BOOLEAN NOT NULL DEFAULT TRUE;
