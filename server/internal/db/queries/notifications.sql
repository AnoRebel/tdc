-- The preference that applies to one conversation.
--
-- The room's own row when there is one, the person's global default
-- otherwise. Ordered so the specific row wins, which is what "override"
-- means: a channel setting beats the default rather than merging with it.
-- name: GetNotificationPreference :one
SELECT level, muted FROM notification_preferences
WHERE user_id = $1 AND (room_id = @room_id::uuid OR room_id IS NULL)
ORDER BY room_id NULLS LAST
LIMIT 1;

-- name: ListNotificationPreferences :many
SELECT room_id, level, muted FROM notification_preferences
WHERE user_id = $1;

-- name: SetGlobalNotificationPreference :exec
INSERT INTO notification_preferences (user_id, room_id, level, muted)
VALUES ($1, NULL, $2, $3)
ON CONFLICT (user_id) WHERE room_id IS NULL
DO UPDATE SET level = EXCLUDED.level, muted = EXCLUDED.muted, updated_at = now();

-- name: SetRoomNotificationPreference :exec
INSERT INTO notification_preferences (user_id, room_id, level, muted)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, room_id) WHERE room_id IS NOT NULL
DO UPDATE SET level = EXCLUDED.level, muted = EXCLUDED.muted, updated_at = now();

-- Raises a notification, once.
--
-- The conflict clause is what stops a redelivered or edited message notifying
-- somebody twice.
-- name: CreateNotification :exec
-- The conflict target matches the constraint: one per person, event, reason
-- and actor. Two people reacting to the same message are two things worth
-- hearing about; the same person reacting twice is one.
INSERT INTO notifications (id, user_id, room_id, event_id, reason, actor_address, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, event_id, reason, actor_address) DO NOTHING;

-- name: ListUnreadNotifications :many
-- The actor comes from the notification rather than the event.
--
-- For a message the two are the same, but for a reaction the event is the
-- message that was reacted to — whose sender is usually the person being
-- notified. Taking the actor from the event would say you reacted to yourself.
SELECT n.id, n.room_id, n.event_id, n.reason, n.created_at,
       n.actor_address, n.detail,
       e.sender_address, e.content
FROM notifications n
JOIN events e ON e.id = n.event_id
WHERE n.user_id = $1 AND n.read_at IS NULL
ORDER BY n.created_at DESC
LIMIT $2;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = now()
WHERE id = $1 AND user_id = $2 AND read_at IS NULL;

-- Withdraws notifications for messages read elsewhere.
--
-- Reading a conversation on a phone must clear what a laptop is showing, which
-- is why this is keyed on the room rather than on individual notifications.
-- name: MarkRoomNotificationsRead :exec
UPDATE notifications SET read_at = now()
WHERE user_id = $1 AND room_id = $2 AND read_at IS NULL;

-- name: ListUnreadNotificationsGrouped :many
-- One entry per conversation, standing for everything unread in it.
--
-- Ten messages in a busy channel is one thing worth knowing about, not ten:
-- a list showing each separately buries every other conversation under the
-- noisiest one, which is the failure that makes people stop reading the list.
--
-- Grouped at read time rather than by writing fewer rows, because each
-- notification must stay individually dismissible — reading one message should
-- withdraw its own notification, not the group's.
--
-- DISTINCT ON takes the newest per room, and the counts describe what it
-- stands for: how many, and how many people are involved.
SELECT DISTINCT ON (n.room_id)
       n.id, n.room_id, n.event_id, n.reason, n.created_at,
       n.actor_address, n.detail,
       e.sender_address, e.content,
       (SELECT count(*) FROM notifications g
         WHERE g.user_id = n.user_id AND g.room_id = n.room_id AND g.read_at IS NULL
       )::bigint AS group_count,
       (SELECT count(DISTINCT g.actor_address) FROM notifications g
         WHERE g.user_id = n.user_id AND g.room_id = n.room_id AND g.read_at IS NULL
       )::bigint AS actor_count
FROM notifications n
JOIN events e ON e.id = n.event_id
WHERE n.user_id = $1 AND n.read_at IS NULL
ORDER BY n.room_id, n.created_at DESC;

-- name: MarkRoomNotificationsReadForUser :exec
-- Dismisses everything unread in one conversation.
--
-- What a grouped entry needs: dismissing the summary must dismiss what it
-- stood for, or the group reappears the moment the list is redrawn.
UPDATE notifications SET read_at = now()
WHERE user_id = $1 AND room_id = $2 AND read_at IS NULL;
