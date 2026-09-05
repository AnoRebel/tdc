ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_once;
ALTER TABLE notifications ADD CONSTRAINT notifications_once
    UNIQUE (user_id, event_id);

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN ('mention', 'direct', 'thread'));

ALTER TABLE notifications DROP COLUMN IF EXISTS detail;
ALTER TABLE notifications DROP COLUMN IF EXISTS actor_address;
