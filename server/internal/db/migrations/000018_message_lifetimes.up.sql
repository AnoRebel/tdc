-- A lifetime attached to one message, independent of the room's own policy.
--
-- Held beside the event rather than inside its content, because content is
-- blanked when the message expires and the record of why it went must survive
-- that. It also keeps the signed event exactly as it was signed.
CREATE TABLE message_lifetimes (
    event_id TEXT PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    room_id  UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,

    -- Who set it. Only they or a moderator may change it, and only to shorten.
    sender_id UUID REFERENCES users(id) ON DELETE SET NULL,

    /*
        How the lifetime is reckoned.

        'after_sending'  — a duration from when it was sent.
        'once_seen'      — as soon as every other participant has read it.
        'after_seeing'   — a duration after each recipient first reads it,
                           which expires when the last of those has elapsed.

        Three kinds rather than one with special values: "expires when seen"
        and "expires an hour after seen" answer different questions, and
        collapsing them into a duration of zero would make the difference
        between them invisible to anybody reading the table.
    */
    kind TEXT NOT NULL,

    -- Seconds, for the two kinds that use a duration. Null for 'once_seen'.
    seconds INTEGER,

    -- When this message becomes removable, once that is known.
    --
    -- Computed rather than derived on read: 'after_sending' knows it
    -- immediately, while the seen-based kinds cannot know it until people have
    -- read, and a sweep that had to reason about all three every pass would
    -- reason about them wrongly eventually.
    expires_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT message_lifetimes_kind_valid
        CHECK (kind IN ('after_sending', 'once_seen', 'after_seeing')),
    -- A duration is required where the kind uses one, and meaningless where it
    -- does not.
    CONSTRAINT message_lifetimes_seconds_match CHECK (
        (kind = 'once_seen' AND seconds IS NULL)
        OR (kind <> 'once_seen' AND seconds IS NOT NULL AND seconds > 0)
    )
);

-- The sweep asks for what is due, so that is the index it gets.
CREATE INDEX message_lifetimes_due_idx ON message_lifetimes (expires_at)
    WHERE expires_at IS NOT NULL;

-- Resolving the seen-based kinds walks the lifetimes of one room at a time.
CREATE INDEX message_lifetimes_room_idx ON message_lifetimes (room_id)
    WHERE expires_at IS NULL;

/*
When each recipient first saw a message that expires after seeing.

Only for 'after_seeing', where each reader starts their own clock and the
message goes when the last of them has elapsed. The other two kinds need
nothing per reader: 'after_sending' ignores reading, and 'once_seen' is
answered by the read marks the room already keeps.
*/
CREATE TABLE message_views (
    event_id  TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    viewer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (event_id, viewer_id)
);
