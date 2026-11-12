-- Automatic replies: what the server says on somebody's behalf while they are
-- away, and a record of whom it has already said it to.
--
-- A person may hold several, each tied to statuses, a window of time, or both;
-- the first that applies is the one that answers.
CREATE TABLE automatic_replies (
    id              UUID        PRIMARY KEY,
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    -- What a direct message is answered with. Markdown, as any message is.
    message         TEXT        NOT NULL,
    -- What a mention is answered with; empty means the same as message.
    mention_message TEXT        NOT NULL DEFAULT '',
    -- Status keys that turn this on; empty means whatever the status is.
    statuses        TEXT[]      NOT NULL DEFAULT '{}',
    -- The window, as instants. Either end may be open: no start is "now",
    -- no end is "until I change it".
    starts_at       TIMESTAMPTZ,
    ends_at         TIMESTAMPTZ,
    -- The zone the window was chosen in, so it is shown back as it was meant.
    time_zone       TEXT        NOT NULL DEFAULT 'UTC',
    reply_to_direct BOOLEAN     NOT NULL DEFAULT TRUE,
    reply_to_groups BOOLEAN     NOT NULL DEFAULT FALSE,
    reply_to_mentions BOOLEAN   NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (char_length(message) BETWEEN 1 AND 500),
    CHECK (char_length(mention_message) <= 500),
    CHECK (starts_at IS NULL OR ends_at IS NULL OR starts_at < ends_at)
);

CREATE INDEX automatic_replies_user_idx ON automatic_replies (user_id, created_at);

-- Whom somebody's automatic reply has answered, and where.
--
-- One row per person, sender and conversation: the reply goes once, and again
-- only after a day has passed. Claimed by an upsert, so two messages arriving
-- together cannot both be answered.
CREATE TABLE automatic_reply_log (
    user_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id   UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, room_id, sender_id)
);

CREATE INDEX automatic_reply_log_recent_idx ON automatic_reply_log (user_id, sent_at);
