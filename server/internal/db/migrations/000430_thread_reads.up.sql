-- How far each person has read each thread, and whether they follow it.
--
-- A thread's replies are left out of the channel's unread count, so they need
-- a read position of their own. Following is what decides who hears about a
-- reply: taking part follows a thread, and "chosen" records that somebody
-- decided for themselves, which taking part afterwards never overrides.
CREATE TABLE thread_reads (
    root_event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_sequence BIGINT NOT NULL DEFAULT 0,
    following BOOLEAN NOT NULL DEFAULT false,
    chosen BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (user_id, root_event_id)
);

CREATE INDEX thread_reads_followers_idx ON thread_reads (root_event_id) WHERE following;

-- Which thread somebody is typing in, so a thread shows its own writers and
-- the channel does not show them.
ALTER TABLE typing_indicators ADD COLUMN thread_root_id TEXT;
