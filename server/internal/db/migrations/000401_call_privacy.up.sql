/*
Who may call somebody, beside who may write to them.

The same three answers as direct messages — anybody, people they share a
conversation with, nobody — because ringing somebody is the more intrusive of
the two and deserves at least the same say. Separately, whether people on other
servers may, which the operator's own switch for the whole server overrides.
*/
ALTER TABLE user_privacy
    ADD COLUMN calls TEXT NOT NULL DEFAULT 'anyone',
    ADD COLUMN calls_from_other_servers BOOLEAN NOT NULL DEFAULT TRUE,
    ADD CONSTRAINT user_privacy_calls_known CHECK (calls IN ('anyone', 'shared', 'nobody'));

-- The event a call began with, so a missed call can point at it.
ALTER TABLE calls ADD COLUMN started_event TEXT REFERENCES events(id) ON DELETE SET NULL;

-- A call nobody answered.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN (
        'mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin',
        'webhook_disabled',
        'assigned',
        'missed_call'
    ));
