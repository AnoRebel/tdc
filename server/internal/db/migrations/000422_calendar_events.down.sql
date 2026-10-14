DELETE FROM role_permissions
WHERE permission IN ('event.create', 'event.manage.any', 'event.calendars.manage');

DELETE FROM notifications
WHERE reason IN ('event_invite', 'event_reminder', 'event_changed', 'event_cancelled', 'event_promoted');
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN (
        'mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin',
        'webhook_disabled',
        'assigned',
        'missed_call'
    ));

ALTER TABLE user_privacy DROP COLUMN shows_event_attendance;

DROP TABLE calendar_feeds;
DROP TABLE calendar_reminders;
DROP TABLE calendar_invites;
DROP TABLE calendar_rsvps;
DROP TABLE calendar_sessions;
DROP TABLE calendar_event_people;
DROP TABLE calendar_events;
DROP TABLE calendar_subscriptions;

DELETE FROM rooms WHERE kind = 'event';
ALTER TABLE rooms DROP CONSTRAINT rooms_kind_valid;
ALTER TABLE rooms ADD CONSTRAINT rooms_kind_valid
    CHECK (kind IN ('channel', 'direct', 'disposable', 'project', 'issue-discussion'));
