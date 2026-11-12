-- name: CreateCalendarEvent :one
INSERT INTO calendar_events (
    id, channel_id, kind, title, description, starts_at, ends_at, time_zone, all_day,
    recurrence, exceptions, last_ends_at, location, online, online_url, capacity,
    visibility, uid, sequence, subscription_id, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
)
RETURNING *;

-- name: UpdateCalendarEvent :one
-- Every field an edit can change, and the sequence that tells a calendar
-- application its copy is out of date.
UPDATE calendar_events SET
    channel_id = $2, kind = $3, title = $4, description = $5, starts_at = $6, ends_at = $7,
    time_zone = $8, all_day = $9, recurrence = $10, exceptions = $11, last_ends_at = $12,
    location = $13, online = $14, online_url = $15, capacity = $16, visibility = $17,
    sequence = sequence + 1, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetCalendarEventCover :exec
UPDATE calendar_events SET cover_attachment_id = $2, updated_at = NOW() WHERE id = $1;

-- name: CancelCalendarEvent :one
UPDATE calendar_events
SET cancelled_at = NOW(), cancel_reason = $2, sequence = sequence + 1, updated_at = NOW()
WHERE id = $1 AND cancelled_at IS NULL
RETURNING *;

-- name: RestoreCalendarEvent :one
UPDATE calendar_events
SET cancelled_at = NULL, cancel_reason = '', sequence = sequence + 1, updated_at = NOW()
WHERE id = $1 AND cancelled_at IS NOT NULL
RETURNING *;

-- name: GetCalendarEvent :one
SELECT * FROM calendar_events WHERE id = $1;

-- name: LockCalendarEvent :one
-- Held for the length of an answer, so two people taking the last place
-- serialise here and the second finds it gone.
SELECT * FROM calendar_events WHERE id = $1 FOR UPDATE;

-- name: GetCalendarEventByUID :one
SELECT * FROM calendar_events
WHERE uid = $1
  AND COALESCE(subscription_id, '00000000-0000-0000-0000-000000000000'::uuid)
      = COALESCE(sqlc.narg(subscription_id)::uuid, '00000000-0000-0000-0000-000000000000'::uuid);

-- name: ListCalendarEventsInRange :many
-- Candidates whose occurrences may fall in the range, with the channel each
-- belongs to. Who may see each is decided by the caller, from the same facts
-- every other route uses; this only narrows by time and place.
SELECT sqlc.embed(e),
       COALESCE(c.name, '')::text       AS channel_name,
       COALESCE(c.visibility, '')::text AS channel_visibility
FROM calendar_events e
LEFT JOIN rooms c ON c.id = e.channel_id
WHERE e.starts_at < sqlc.arg(until)::timestamptz
  AND (e.last_ends_at IS NULL OR e.last_ends_at > sqlc.arg(since)::timestamptz)
  AND (sqlc.narg(channel_id)::uuid IS NULL OR e.channel_id = sqlc.narg(channel_id)::uuid)
ORDER BY e.starts_at
LIMIT 2000;

-- name: GetCalendarEventWithChannel :one
SELECT sqlc.embed(e),
       COALESCE(c.name, '')::text       AS channel_name,
       COALESCE(c.visibility, '')::text AS channel_visibility
FROM calendar_events e
LEFT JOIN rooms c ON c.id = e.channel_id
WHERE e.id = $1;

-- name: ListCalendarEventsByIDs :many
SELECT sqlc.embed(e),
       COALESCE(c.name, '')::text       AS channel_name,
       COALESCE(c.visibility, '')::text AS channel_visibility
FROM calendar_events e
LEFT JOIN rooms c ON c.id = e.channel_id
WHERE e.id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY e.starts_at;

-- name: ListCalendarEventsInChannels :many
-- Every event of the given channels that has not long ended, for a feed.
SELECT sqlc.embed(e),
       COALESCE(c.name, '')::text       AS channel_name,
       COALESCE(c.visibility, '')::text AS channel_visibility
FROM calendar_events e
LEFT JOIN rooms c ON c.id = e.channel_id
WHERE e.channel_id = ANY(sqlc.arg(channels)::uuid[])
  AND (e.last_ends_at IS NULL OR e.last_ends_at > sqlc.arg(since)::timestamptz)
ORDER BY e.starts_at
LIMIT 2000;

-- name: ListPublicCalendarEvents :many
SELECT sqlc.embed(e),
       COALESCE(c.name, '')::text       AS channel_name,
       COALESCE(c.visibility, '')::text AS channel_visibility
FROM calendar_events e
LEFT JOIN rooms c ON c.id = e.channel_id
WHERE e.visibility = 'public'
  AND (c.id IS NULL OR c.visibility = 'public')
  AND (e.last_ends_at IS NULL OR e.last_ends_at > sqlc.arg(since)::timestamptz)
ORDER BY e.starts_at
LIMIT 2000;

-- name: ListCalendarEventsForReminders :many
-- Events that may have an occurrence starting soon: not cancelled, and either
-- repeating or starting within the window.
SELECT * FROM calendar_events
WHERE cancelled_at IS NULL
  AND (last_ends_at IS NULL OR last_ends_at > NOW())
  AND (recurrence <> '' OR (starts_at > NOW() AND starts_at <= sqlc.arg(until)::timestamptz))
LIMIT 5000;

-- name: DeleteCalendarEvent :exec
-- The room goes, and the event with it.
DELETE FROM rooms WHERE id = $1 AND kind = 'event';

-- name: CalendarEventsInvolving :many
-- The events somebody is in some way part of: hosting or speaking, having
-- answered, or invited — themselves or through a channel they are in.
SELECT event_id, 'host'::text AS how FROM calendar_event_people WHERE calendar_event_people.user_id = $1
UNION ALL
SELECT event_id, 'answered'::text FROM calendar_rsvps WHERE calendar_rsvps.user_id = $1
UNION ALL
SELECT event_id, 'invited'::text FROM calendar_invites WHERE calendar_invites.user_id = $1
UNION ALL
SELECT i.event_id, 'invited'::text FROM calendar_invites i
JOIN room_members m ON m.room_id = i.channel_id AND m.user_id = $1;

-- name: ListCalendarEventPeople :many
SELECT p.event_id, p.user_id, p.role, u.username, u.display_name
FROM calendar_event_people p
JOIN users u ON u.id = p.user_id
WHERE p.event_id = $1
ORDER BY p.role, u.username;

-- name: ClearCalendarEventPeople :exec
DELETE FROM calendar_event_people WHERE event_id = $1;

-- name: AddCalendarEventPerson :exec
INSERT INTO calendar_event_people (event_id, user_id, role) VALUES ($1, $2, $3)
ON CONFLICT (event_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: ListCalendarSessions :many
SELECT * FROM calendar_sessions WHERE event_id = $1 ORDER BY starts_at, title;

-- name: ClearCalendarSessions :exec
DELETE FROM calendar_sessions WHERE event_id = $1;

-- name: AddCalendarSession :exec
INSERT INTO calendar_sessions (id, event_id, title, description, starts_at, ends_at, speakers, guest_speakers)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetCalendarRSVP :one
SELECT * FROM calendar_rsvps WHERE event_id = $1 AND user_id = $2;

-- name: SetCalendarRSVP :exec
-- The time answered changes only when the answer does, so changing nothing
-- does not send somebody to the back of a waiting list.
INSERT INTO calendar_rsvps (event_id, user_id, status) VALUES ($1, $2, $3)
ON CONFLICT (event_id, user_id) DO UPDATE
SET status = EXCLUDED.status,
    responded_at = CASE WHEN calendar_rsvps.status = EXCLUDED.status
                        THEN calendar_rsvps.responded_at ELSE NOW() END;

-- name: CountCalendarRSVPs :many
SELECT status, count(*)::bigint AS people FROM calendar_rsvps WHERE event_id = $1 GROUP BY status;

-- name: CountCalendarGoing :one
-- Hosts and speakers do not take places.
SELECT count(*)::bigint FROM calendar_rsvps r
WHERE r.event_id = $1 AND r.status = 'going'
  AND NOT EXISTS (SELECT 1 FROM calendar_event_people p WHERE p.event_id = r.event_id AND p.user_id = r.user_id);

-- name: FirstWaitlisted :one
SELECT user_id FROM calendar_rsvps
WHERE event_id = $1 AND status = 'waitlisted'
ORDER BY responded_at, user_id
LIMIT 1;

-- name: WaitlistPosition :one
SELECT count(*)::bigint FROM calendar_rsvps w
WHERE w.event_id = $1 AND w.status = 'waitlisted'
  AND (w.responded_at, w.user_id) <= (
      SELECT r.responded_at, r.user_id FROM calendar_rsvps r WHERE r.event_id = $1 AND r.user_id = $2
  );

-- name: ListCalendarAttendees :many
-- Everybody who answered, with whether they let others see it. Withholding is
-- the caller's to apply, by role.
SELECT r.user_id, r.status, r.responded_at, u.username, u.display_name,
       COALESCE(pv.shows_event_attendance, TRUE)::boolean AS discloses
FROM calendar_rsvps r
JOIN users u ON u.id = r.user_id
LEFT JOIN user_privacy pv ON pv.user_id = r.user_id
WHERE r.event_id = $1
ORDER BY r.status, r.responded_at;

-- name: ListCalendarRecipients :many
-- Who hears about an occurrence: people going or maybe going, and hosts and
-- speakers.
SELECT r.user_id FROM calendar_rsvps r WHERE r.event_id = $1 AND r.status IN ('going', 'maybe')
UNION
SELECT p.user_id FROM calendar_event_people p WHERE p.event_id = $1;

-- name: ShowsEventAttendance :one
SELECT COALESCE((SELECT shows_event_attendance FROM user_privacy WHERE user_id = $1), TRUE)::boolean;

-- name: SetShowsEventAttendance :exec
INSERT INTO user_privacy (user_id, shows_event_attendance, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE SET shows_event_attendance = EXCLUDED.shows_event_attendance, updated_at = NOW();

-- name: CreateCalendarInvite :one
INSERT INTO calendar_invites (id, event_id, user_id, channel_id, invited_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ListCalendarInvites :many
SELECT i.*, COALESCE(u.username, '')::text AS username, COALESCE(u.display_name, '')::text AS display_name, COALESCE(c.name, '')::text AS channel_name
FROM calendar_invites i
LEFT JOIN users u ON u.id = i.user_id
LEFT JOIN rooms c ON c.id = i.channel_id
WHERE i.event_id = $1
ORDER BY i.created_at;

-- name: DeleteCalendarInvite :execrows
DELETE FROM calendar_invites WHERE id = $1 AND event_id = $2;

-- name: ClaimCalendarReminder :execrows
INSERT INTO calendar_reminders (event_id, occurrence, lead_minutes) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: SetCalendarFeed :exec
INSERT INTO calendar_feeds (user_id, token_hash) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, created_at = NOW(), last_used_at = NULL;

-- name: GetCalendarFeed :one
SELECT * FROM calendar_feeds WHERE user_id = $1;

-- name: UseCalendarFeed :one
UPDATE calendar_feeds SET last_used_at = NOW() WHERE token_hash = $1
RETURNING user_id;

-- name: DeleteCalendarFeed :execrows
DELETE FROM calendar_feeds WHERE user_id = $1;

-- name: CreateCalendarSubscription :one
INSERT INTO calendar_subscriptions (id, url, label, channel_id, visibility, kind, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListCalendarSubscriptions :many
SELECT * FROM calendar_subscriptions ORDER BY created_at;

-- name: GetCalendarSubscription :one
SELECT * FROM calendar_subscriptions WHERE id = $1;

-- name: DeleteCalendarSubscription :execrows
DELETE FROM calendar_subscriptions WHERE id = $1;

-- name: ClaimDueCalendarSubscription :one
-- One subscription whose turn has come, pushed into the future as it is
-- claimed, so another node looking at the same moment finds nothing to do.
UPDATE calendar_subscriptions SET next_fetch_at = NOW() + make_interval(secs => sqlc.arg(hold_seconds)::int)
WHERE id = (
    SELECT id FROM calendar_subscriptions
    WHERE paused_at IS NULL AND next_fetch_at <= NOW()
    ORDER BY next_fetch_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: RecordCalendarFetch :exec
UPDATE calendar_subscriptions
SET last_fetched_at = NOW(), last_error = $2, failures = $3, paused_at = $4,
    etag = $5, last_modified = $6, next_fetch_at = $7
WHERE id = $1;

-- name: ResumeCalendarSubscription :exec
UPDATE calendar_subscriptions SET paused_at = NULL, failures = 0, next_fetch_at = NOW() WHERE id = $1;

-- name: ListSubscriptionEventUIDs :many
SELECT id, uid, cancelled_at FROM calendar_events WHERE subscription_id = $1;

-- name: IsCalendarCover :one
-- Whether an attachment is some event's cover, which is what keeps it from
-- being reclaimed and lets people who may see the event see it.
SELECT id FROM calendar_events WHERE cover_attachment_id = $1 LIMIT 1;

-- name: ListMyCalendarAnswers :many
SELECT event_id, status FROM calendar_rsvps WHERE user_id = $1;

-- name: CalendarAnchorEvent :one
-- The entry an event began with, which notifications about the event as a
-- whole point at.
SELECT id FROM events WHERE room_id = $1 AND type = 'event.created' ORDER BY sequence LIMIT 1;
