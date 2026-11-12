DELETE FROM notifications WHERE reason = 'missed_call';
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN (
        'mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin',
        'webhook_disabled',
        'assigned'
    ));
ALTER TABLE calls DROP COLUMN started_event;
ALTER TABLE user_privacy
    DROP CONSTRAINT user_privacy_calls_known,
    DROP COLUMN calls_from_other_servers,
    DROP COLUMN calls;
