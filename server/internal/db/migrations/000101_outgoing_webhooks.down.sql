DELETE FROM notifications WHERE reason = 'webhook_disabled';
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN ('mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin'));

DROP TABLE IF EXISTS webhook_attempts;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_subscriptions;
