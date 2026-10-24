/*
Events: meetups, talks, workshops and office hours.

Every event is a room too, of kind 'event', whose id is the event's — the
pattern issues set with their discussions. The room is the event's
conversation: its log records what happens to the event, its members are the
people taking part, and its call is the event's call. Notifications, mutes,
calls and webhooks all key on rooms, and so all of them reach events without
learning what one is.
*/
ALTER TABLE rooms DROP CONSTRAINT rooms_kind_valid;
ALTER TABLE rooms ADD CONSTRAINT rooms_kind_valid
    CHECK (kind IN ('channel', 'direct', 'disposable', 'project', 'issue-discussion', 'event'));

/*
External calendars a server or a channel follows.

Made before events, which point at the subscription they came from. The
address is somebody else's, fetched on a schedule, so everything about the
last fetch is kept for whoever has to work out why it stopped.
*/
CREATE TABLE calendar_subscriptions (
    id               UUID        PRIMARY KEY,
    url              TEXT        NOT NULL,
    label            TEXT        NOT NULL,
    channel_id       UUID        REFERENCES rooms(id) ON DELETE CASCADE,
    -- What the events it brings in are given.
    visibility       TEXT        NOT NULL DEFAULT 'public',
    kind             TEXT        NOT NULL DEFAULT 'other',
    created_by       UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Claimed by a node before it fetches, so two never fetch at once.
    next_fetch_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_fetched_at  TIMESTAMPTZ,
    last_error       TEXT        NOT NULL DEFAULT '',
    failures         INTEGER     NOT NULL DEFAULT 0,
    -- Set after repeated failure; a paused subscription is not fetched until
    -- somebody refreshes it by hand.
    paused_at        TIMESTAMPTZ,
    etag             TEXT        NOT NULL DEFAULT '',
    last_modified    TEXT        NOT NULL DEFAULT '',
    CONSTRAINT calendar_subscriptions_visibility_known CHECK (visibility IN ('public', 'channel')),
    CONSTRAINT calendar_subscriptions_kind_known
        CHECK (kind IN ('meetup', 'talk', 'workshop', 'office_hours', 'other')),
    CONSTRAINT calendar_subscriptions_channel_visibility
        CHECK (visibility <> 'channel' OR channel_id IS NOT NULL)
);

CREATE TABLE calendar_events (
    id                  UUID        PRIMARY KEY REFERENCES rooms(id) ON DELETE CASCADE,
    -- The channel it belongs to, or none for an event of the whole server.
    channel_id          UUID        REFERENCES rooms(id) ON DELETE CASCADE,
    kind                TEXT        NOT NULL,
    title               TEXT        NOT NULL,
    -- Markdown, in the syntax messages use.
    description         TEXT        NOT NULL DEFAULT '',
    -- The first occurrence. Instants; the zone they were meant in is beside
    -- them, and is what a repeat is expanded in.
    starts_at           TIMESTAMPTZ NOT NULL,
    ends_at             TIMESTAMPTZ NOT NULL,
    time_zone           TEXT        NOT NULL,
    all_day             BOOLEAN     NOT NULL DEFAULT FALSE,
    -- An RRULE body ("FREQ=WEEKLY;COUNT=6"), empty for an event that happens once.
    recurrence          TEXT        NOT NULL DEFAULT '',
    -- Occurrence starts that are skipped.
    exceptions          TIMESTAMPTZ[] NOT NULL DEFAULT '{}',
    -- When the last occurrence ends; null for a series without end. What a
    -- range query filters on before occurrences are expanded.
    last_ends_at        TIMESTAMPTZ,
    location            TEXT        NOT NULL DEFAULT '',
    -- Hosted in this server's own call.
    online              BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Or somewhere else.
    online_url          TEXT        NOT NULL DEFAULT '',
    capacity            INTEGER,
    visibility          TEXT        NOT NULL,
    cover_attachment_id UUID        REFERENCES attachments(id) ON DELETE SET NULL,
    -- iCalendar identity. Unique within where an event came from: a file
    -- imported by hand, or one subscription.
    uid                 TEXT        NOT NULL,
    sequence            INTEGER     NOT NULL DEFAULT 0,
    subscription_id     UUID        REFERENCES calendar_subscriptions(id) ON DELETE CASCADE,
    created_by          UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at        TIMESTAMPTZ,
    cancel_reason       TEXT        NOT NULL DEFAULT '',
    CONSTRAINT calendar_events_kind_known
        CHECK (kind IN ('meetup', 'talk', 'workshop', 'office_hours', 'other')),
    CONSTRAINT calendar_events_visibility_known CHECK (visibility IN ('public', 'channel', 'invite')),
    CONSTRAINT calendar_events_channel_visibility CHECK (visibility <> 'channel' OR channel_id IS NOT NULL),
    CONSTRAINT calendar_events_ends_after_start CHECK (ends_at >= starts_at),
    CONSTRAINT calendar_events_capacity_positive CHECK (capacity IS NULL OR capacity > 0)
);

CREATE UNIQUE INDEX calendar_events_uid
    ON calendar_events (COALESCE(subscription_id, '00000000-0000-0000-0000-000000000000'::uuid), uid);
CREATE INDEX calendar_events_range ON calendar_events (starts_at, last_ends_at);
CREATE INDEX calendar_events_channel ON calendar_events (channel_id) WHERE channel_id IS NOT NULL;

-- Hosts and speakers. One role per person per event.
CREATE TABLE calendar_event_people (
    event_id UUID NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role     TEXT NOT NULL,
    PRIMARY KEY (event_id, user_id),
    CONSTRAINT calendar_event_people_role_known CHECK (role IN ('host', 'speaker'))
);

CREATE INDEX calendar_event_people_user ON calendar_event_people (user_id);

-- The agenda: the talks, or parts, within an event.
CREATE TABLE calendar_sessions (
    id             UUID        PRIMARY KEY,
    event_id       UUID        NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
    title          TEXT        NOT NULL,
    description    TEXT        NOT NULL DEFAULT '',
    starts_at      TIMESTAMPTZ NOT NULL,
    ends_at        TIMESTAMPTZ NOT NULL,
    -- Speakers on this server, and in words those who are not.
    speakers       UUID[]      NOT NULL DEFAULT '{}',
    guest_speakers TEXT        NOT NULL DEFAULT '',
    CONSTRAINT calendar_sessions_ends_after_start CHECK (ends_at >= starts_at)
);

CREATE INDEX calendar_sessions_event ON calendar_sessions (event_id, starts_at);

/*
Answers. One per person per event, covering a whole series. The time answered
is what orders the waiting list.
*/
CREATE TABLE calendar_rsvps (
    event_id     UUID        NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT        NOT NULL,
    responded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (event_id, user_id),
    CONSTRAINT calendar_rsvps_status_known CHECK (status IN ('going', 'maybe', 'declined', 'waitlisted'))
);

CREATE INDEX calendar_rsvps_user ON calendar_rsvps (user_id);
CREATE INDEX calendar_rsvps_waiting ON calendar_rsvps (event_id, responded_at) WHERE status = 'waitlisted';

-- Invitations, of one person or one channel's members.
CREATE TABLE calendar_invites (
    id         UUID        PRIMARY KEY,
    event_id   UUID        NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
    user_id    UUID        REFERENCES users(id) ON DELETE CASCADE,
    channel_id UUID        REFERENCES rooms(id) ON DELETE CASCADE,
    invited_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT calendar_invites_one_target CHECK ((user_id IS NULL) <> (channel_id IS NULL))
);

CREATE UNIQUE INDEX calendar_invites_person ON calendar_invites (event_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX calendar_invites_channel ON calendar_invites (event_id, channel_id) WHERE channel_id IS NOT NULL;

/*
Reminders sent. The row is the claim: a node that inserts it sends the
reminder, and a node that finds it already there does not, so a reminder goes
out once however many nodes look.
*/
CREATE TABLE calendar_reminders (
    event_id     UUID        NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
    occurrence   TIMESTAMPTZ NOT NULL,
    lead_minutes INTEGER     NOT NULL,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (event_id, occurrence, lead_minutes)
);

/*
Personal subscription addresses. The secret is in the address and stored only
as its hash; one per person, replaced by making another.
*/
CREATE TABLE calendar_feeds (
    user_id      UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash   BYTEA       NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);

-- Whether others may see which events somebody is going to. Sparse, like the
-- rest of user_privacy: no row is the default, which discloses.
ALTER TABLE user_privacy ADD COLUMN shows_event_attendance BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN (
        'mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin',
        'webhook_disabled',
        'assigned',
        'missed_call',
        -- Events: invited, reminded, changed, cancelled, moved up from waiting.
        'event_invite', 'event_reminder', 'event_changed', 'event_cancelled', 'event_promoted'
    ));

/*
Planning a meetup is what an ordinary member of a developer community does, so
the Member role may create events, as it may create channels. An operator who
wants only some people scheduling withdraws it.
*/
INSERT INTO role_permissions (role_id, permission, scope_id, granted)
SELECT id, 'event.create', NULL, TRUE
FROM roles
WHERE roles.name = 'Member' AND roles.built_in
ON CONFLICT (role_id, permission, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO NOTHING;
