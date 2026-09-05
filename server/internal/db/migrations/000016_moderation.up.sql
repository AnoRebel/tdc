-- Moderation: restrictions on what somebody may do, and where.
--
-- Four things that look similar and are not. A suspension is server-wide and
-- already lives on the users table. These are the rest: a ban from one channel,
-- a restriction on posting, a person blocking another, and a person's own
-- setting for who may write to them.
--
-- Each is separate because each answers a different question, and folding them
-- into one table would mean a query for "may this person post here" reading
-- rows about who they have blocked.

/*
  Channel bans, temporary or permanent.

  Confined to their channel: a ban from one conversation says nothing about any
  other, and a person banned from one channel keeps every other membership.
  That confinement is the difference between a ban and a suspension.
*/
CREATE TABLE channel_bans (
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    banned_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    -- Kept as an address so the record survives the moderator's account being
    -- removed, which is when an old decision most needs attributing.
    banned_by_address TEXT NOT NULL,

    -- Why. Required, because a ban nobody can explain is a ban nobody can
    -- review — and the person banned is entitled to the reason.
    reason     TEXT        NOT NULL,

    -- When it lifts. Null is permanent. A temporary ban expires without
    -- anybody acting, which is what makes "banned for a week" a thing a
    -- moderator can actually do.
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (room_id, user_id)
);

-- Finding bans that have lapsed, for the sweep that clears them.
CREATE INDEX channel_bans_expiry_idx ON channel_bans (expires_at)
    WHERE expires_at IS NOT NULL;

/*
  Posting restrictions: present but silenced.

  Distinct from a ban, which removes somebody. A restriction lets them read a
  conversation they are part of while stopping them adding to it, which is the
  proportionate response to somebody who is arguing rather than attacking.

  A null room restricts them everywhere on the server.
*/
CREATE TABLE posting_restrictions (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Null means the whole server.
    room_id    UUID        REFERENCES rooms(id) ON DELETE CASCADE,

    restricted_by UUID     REFERENCES users(id) ON DELETE SET NULL,
    restricted_by_address TEXT NOT NULL,
    reason     TEXT        NOT NULL,

    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Lifted early, by a moderator, as distinct from expiring on its own.
    lifted_at  TIMESTAMPTZ,
    lifted_by  UUID        REFERENCES users(id) ON DELETE SET NULL
);

-- The question asked on every message: is this person restricted here, now.
CREATE INDEX posting_restrictions_active_idx
    ON posting_restrictions (user_id, room_id)
    WHERE lifted_at IS NULL;

/*
  One person blocking another.

  Never disclosed to the person blocked. They are not told, their messages are
  not refused in a way that reveals it, and nothing in the interface says so —
  because somebody who learns they have been blocked frequently escalates, and
  the point of blocking is to end contact rather than to announce it.
*/
CREATE TABLE user_blocks (
    blocker_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (blocker_id, blocked_id),
    -- Blocking yourself is not a thing anybody means to do.
    CONSTRAINT user_blocks_not_self CHECK (blocker_id <> blocked_id)
);

-- Whether a particular person has blocked another, asked before delivery.
CREATE INDEX user_blocks_blocked_idx ON user_blocks (blocked_id);

/*
  Who may open a direct conversation with somebody.

  A column on a settings table rather than on users, because it is one of
  several per-person preferences and putting each on the users table would mean
  a migration for every new one.
*/
CREATE TABLE user_privacy (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    -- 'anyone', 'shared' (only people sharing a channel), or 'nobody'.
    direct_messages TEXT NOT NULL DEFAULT 'anyone',

    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT user_privacy_dm_known
        CHECK (direct_messages IN ('anyone', 'shared', 'nobody'))
);
