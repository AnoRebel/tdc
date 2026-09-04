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
INSERT INTO notifications (id, user_id, room_id, event_id, reason)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, event_id) DO NOTHING;

-- name: ListUnreadNotifications :many
SELECT n.id, n.room_id, n.event_id, n.reason, n.created_at,
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
