-- Notifications, and the preferences that govern them.

-- What a person is told about, and where.
--
-- Preferences are per conversation with a global default, because the useful
-- shape is "tell me about everything except this one busy channel" rather than
-- a single switch. Absent rows mean the global default applies, so muting is
-- the only thing that costs a row.
CREATE TABLE notification_preferences (
    user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Null is the person's global default, which every conversation inherits.
    room_id  UUID REFERENCES rooms(id) ON DELETE CASCADE,

    -- What to be told about: 'all', 'mentions', or 'none'.
    --
    -- 'mentions' is the useful middle that a plain mute lacks: a busy channel
    -- somebody still wants to be pulled into when named.
    level    TEXT NOT NULL DEFAULT 'mentions'
             CHECK (level IN ('all', 'mentions', 'none')),

    -- Muting silences notifications without affecting unread state. Somebody
    -- who mutes a channel is asking not to be interrupted, not to be told
    -- nothing happened — the badge still counts.
    muted    BOOLEAN NOT NULL DEFAULT false,

    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per person per conversation, and one global row each.
--
-- A partial unique index rather than a primary key, because NULL is how the
-- global default is expressed and NULL never equals NULL in a key.
CREATE UNIQUE INDEX notification_preferences_room_idx
    ON notification_preferences (user_id, room_id) WHERE room_id IS NOT NULL;
CREATE UNIQUE INDEX notification_preferences_global_idx
    ON notification_preferences (user_id) WHERE room_id IS NULL;

-- What somebody has been told about.
--
-- Stored rather than only pushed, so that a notification survives a client
-- being closed, arrives on a second device, and can be withdrawn when the
-- message is read somewhere else.
CREATE TABLE notifications (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    event_id   TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,

    -- Why this was raised: 'mention', 'direct', or 'thread'.
    reason     TEXT        NOT NULL CHECK (reason IN ('mention', 'direct', 'thread')),

    -- Read here or elsewhere. A notification read on a phone must stop being
    -- shown on a laptop, which is why this is server state rather than each
    -- client's own.
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One notification per person per message. A message edited or delivered
    -- twice must not notify twice.
    CONSTRAINT notifications_once UNIQUE (user_id, event_id)
);

-- The common read: what is unread, newest first.
CREATE INDEX notifications_unread_idx ON notifications (user_id, created_at DESC)
    WHERE read_at IS NULL;

-- Withdrawing a notification when its message is read.
CREATE INDEX notifications_event_idx ON notifications (event_id);
