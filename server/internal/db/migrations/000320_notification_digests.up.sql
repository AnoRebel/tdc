/*
Somebody's choice to be sent a summary of what they missed, instead of being
interrupted as it happens.

One row per person who has ever chosen, kept when they turn it off so their
hour and zone are still there if they turn it back on. Nobody has a row until
they choose, which is what "off by default" is here.

The schedule is stored as the person put it — an hour of the day, a day of the
week, a zone name — and the next instant it falls on is computed from that in
the server. Storing only the instant would lose what "eight in the morning"
meant the first time a clock changed for the summer.

covered_until is the boundary between what has been summarised and what has
not. It starts at the moment somebody opts in, so the first summary does not
reach back over everything they ever missed, and each summary moves it forward
to the moment that summary was taken.

claimed_until is how several servers share one database without anybody being
mailed twice: a pass claims the rows it will send in the statement that finds
them, and the claim keeps every other pass off them until it lapses.
*/
CREATE TABLE notification_digests (
    user_id       UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    frequency     TEXT        NOT NULL DEFAULT 'off'
                              CHECK (frequency IN ('off', 'daily', 'weekly')),
    -- The hour of the day in time_zone, 0 to 23.
    hour          SMALLINT    NOT NULL DEFAULT 8 CHECK (hour BETWEEN 0 AND 23),
    -- The day of the week for a weekly summary, Sunday being 0.
    weekday       SMALLINT    NOT NULL DEFAULT 1 CHECK (weekday BETWEEN 0 AND 6),
    -- An IANA zone name, validated by the server before it is stored.
    time_zone     TEXT        NOT NULL DEFAULT 'UTC',
    next_due_at   TIMESTAMPTZ,
    covered_until TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_until TIMESTAMPTZ,
    last_sent_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- What a delivery pass looks for: summaries that are switched on and due.
CREATE INDEX notification_digests_due_idx ON notification_digests (next_due_at)
    WHERE frequency <> 'off';

/*
What batching asks on every notification: has this person already been told
about this conversation, recently, and not yet read it.
*/
CREATE INDEX notifications_unread_room_idx ON notifications (user_id, room_id, created_at DESC)
    WHERE read_at IS NULL;
