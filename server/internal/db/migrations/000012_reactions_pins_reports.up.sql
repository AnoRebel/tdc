-- Reactions, pins, reports, scheduled messages, and attachment captions.
--
-- One migration because they share a shape: each is a small record pointing at
-- an event, and splitting them would mean five migrations that all have to be
-- applied together anyway to give a usable version.

-- A caption belongs to the attachment, not the message.
--
-- Sending four photographs with a line about each is the ordinary case, and a
-- single message body cannot say which line describes which picture.
ALTER TABLE attachments ADD COLUMN caption TEXT NOT NULL DEFAULT '';

-- Reactions.
--
-- The emoji is stored as text rather than as a code point or a name: a short
-- string is what the client sends, what gets displayed, and what other systems
-- interoperate on, and normalising it would mean maintaining a table of every
-- emoji that exists.
CREATE TABLE reactions (
    id         UUID        PRIMARY KEY,
    event_id   TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The address is kept alongside the identifier for the same reason events
    -- keep a sender address: it survives the account and works across servers.
    user_address TEXT      NOT NULL,
    emoji      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One person reacts with one emoji once. Clicking again removes it rather
    -- than counting twice, and this is what makes that a database rule rather
    -- than something the application has to remember.
    CONSTRAINT reactions_once UNIQUE (event_id, user_id, emoji),
    -- A bounded length: an emoji is a handful of code points, and a sequence
    -- with many joiners is still short. This stops the column being used as
    -- free text.
    CONSTRAINT reactions_emoji_length CHECK (char_length(emoji) BETWEEN 1 AND 64)
);

-- Reading a message's reactions is the common query.
CREATE INDEX reactions_event_idx ON reactions (event_id);

-- Pins.
--
-- A pinned message belongs to a room rather than to a person: pinning is how a
-- channel says "this is the important one", so everybody sees the same set.
CREATE TABLE pinned_messages (
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    event_id   TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    pinned_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    pinned_by_address TEXT NOT NULL,
    pinned_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (room_id, event_id)
);

CREATE INDEX pinned_messages_room_idx ON pinned_messages (room_id, pinned_at DESC);

-- A person's own pinned conversations, which is a different thing from a
-- pinned message: this is "keep this channel at the top of my list", so it is
-- per person and carries no meaning for anybody else.
CREATE TABLE pinned_rooms (
    user_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id   UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    -- An explicit order, so somebody can arrange their pinned conversations
    -- rather than having them ordered by when they were pinned.
    position  INTEGER     NOT NULL DEFAULT 0,
    pinned_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, room_id)
);

-- Reports.
--
-- Kept after the reported message is deleted, which is the point: somebody who
-- posts something and removes it should not thereby erase the report about it.
-- The event reference is deliberately not a foreign key for that reason.
CREATE TABLE reports (
    id            UUID        PRIMARY KEY,
    room_id       UUID        REFERENCES rooms(id) ON DELETE SET NULL,
    -- No foreign key: a report outlives the message it is about.
    event_id      TEXT,
    -- Who is being reported, kept as an address so the record survives the
    -- account being removed.
    subject_address TEXT      NOT NULL,
    reporter_id   UUID        REFERENCES users(id) ON DELETE SET NULL,
    reporter_address TEXT     NOT NULL,
    -- A short reason from a fixed set, plus whatever the reporter wanted to
    -- add. The fixed part is what makes a queue sortable.
    reason        TEXT        NOT NULL,
    detail        TEXT        NOT NULL DEFAULT '',
    -- A copy of what was reported, because moderation has to work after the
    -- message is gone.
    reported_body TEXT        NOT NULL DEFAULT '',
    status        TEXT        NOT NULL DEFAULT 'open',
    resolved_by   UUID        REFERENCES users(id) ON DELETE SET NULL,
    resolution    TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ,

    CONSTRAINT reports_status_known CHECK (status IN ('open', 'resolved', 'dismissed')),
    CONSTRAINT reports_reason_known CHECK (
        reason IN ('spam', 'harassment', 'hate', 'violence', 'sexual', 'illegal', 'other')
    )
);

-- The moderation queue: open reports, oldest first, so nothing is left waiting
-- indefinitely behind newer arrivals.
CREATE INDEX reports_open_idx ON reports (created_at)
    WHERE status = 'open';

-- Scheduled messages.
--
-- Held here rather than in events, because an event is a fact that happened
-- and a scheduled message has not happened yet. It becomes an event when it is
-- sent, which is also what stops a scheduled message being visible in a
-- timeline before its time.
CREATE TABLE scheduled_messages (
    id          UUID        PRIMARY KEY,
    room_id     UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sender_address TEXT     NOT NULL,
    body        TEXT        NOT NULL,
    -- Attachments are claimed at send time, so they are recorded by identifier
    -- rather than bound to an event that does not exist yet.
    attachment_ids UUID[]   NOT NULL DEFAULT '{}',
    -- The thread this belongs to, so a scheduled reply stays a reply.
    thread_root_id TEXT,
    send_at     TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Set once it has been sent, so a delivery pass can find what is still
    -- outstanding without deleting the record immediately.
    sent_event_id TEXT,
    -- Recorded rather than retried forever: a scheduled message that cannot be
    -- sent should tell the person who scheduled it.
    failed_reason TEXT       NOT NULL DEFAULT ''
);

-- What is due: the delivery pass reads exactly this.
CREATE INDEX scheduled_messages_due_idx ON scheduled_messages (send_at)
    WHERE sent_event_id IS NULL AND failed_reason = '';

-- Somebody's own scheduled messages, to list and to cancel.
CREATE INDEX scheduled_messages_sender_idx ON scheduled_messages (sender_id, send_at);
