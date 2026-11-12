-- name: GetNotificationDigest :one
SELECT * FROM notification_digests WHERE user_id = $1;

/*
Records somebody's choice.

Switching on from off starts the summary from now, so the first one does not
reach back over everything that ever went unread; changing the hour or the
zone of a summary already on keeps what it has not yet covered.
*/
-- name: SetNotificationDigest :one
INSERT INTO notification_digests (user_id, frequency, hour, weekday, time_zone, next_due_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id) DO UPDATE
SET frequency     = EXCLUDED.frequency,
    hour          = EXCLUDED.hour,
    weekday       = EXCLUDED.weekday,
    time_zone     = EXCLUDED.time_zone,
    next_due_at   = EXCLUDED.next_due_at,
    covered_until = CASE WHEN notification_digests.frequency = 'off'
                         THEN now()
                         ELSE notification_digests.covered_until END,
    updated_at    = now()
RETURNING *;

-- name: NotificationDigestChosen :one
-- Whether somebody has asked for a summary instead of interruptions.
SELECT EXISTS (
    SELECT 1 FROM notification_digests WHERE user_id = $1 AND frequency <> 'off'
)::boolean;

/*
What is due, claimed by whoever asked.

One statement, so finding a row and claiming it cannot be separated: SKIP
LOCKED keeps a concurrent pass off the rows this one is claiming, and the claim
keeps every later pass off them until it lapses. Five minutes is far longer
than composing and sending a batch takes, and short enough that a summary held
by a server that stopped goes out soon after.
*/
-- name: ClaimDueNotificationDigests :many
UPDATE notification_digests
SET claimed_until = now() + interval '5 minutes'
WHERE user_id IN (
    SELECT d.user_id FROM notification_digests d
    WHERE d.frequency <> 'off' AND d.next_due_at <= now()
      AND (d.claimed_until IS NULL OR d.claimed_until < now())
    ORDER BY d.next_due_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

/*
Done with one person's summary for this period, whether or not anything was
sent: what it covered is not summarised again, and the next one is scheduled.
*/
-- name: CompleteNotificationDigest :exec
UPDATE notification_digests
SET covered_until = $2,
    next_due_at   = $3,
    last_sent_at  = CASE WHEN @sent::boolean THEN now() ELSE last_sent_at END,
    claimed_until = NULL
WHERE user_id = $1;

/*
Not now: their status says not to interrupt them. What it would have covered
is left uncovered, so it goes in the next attempt rather than being lost.
*/
-- name: PostponeNotificationDigest :exec
UPDATE notification_digests
SET next_due_at = $2, claimed_until = NULL
WHERE user_id = $1;

/*
What somebody has not read, per conversation, inside a summary's window.

Everything unread rather than only what was never pushed: a summary is of what
somebody missed, and a notification they were shown but never opened is
exactly that. Read ones are left out, since reading is what "missed" ends.
*/
-- name: ListDigestActivity :many
SELECT n.room_id,
       r.name,
       r.kind,
       count(*)::bigint                                           AS notifications,
       count(DISTINCT n.actor_address)::bigint                    AS actors,
       count(*) FILTER (WHERE n.reason IN ('mention', 'direct'))::bigint AS addressed,
       max(n.created_at)::timestamptz                             AS latest
FROM notifications n
JOIN rooms r ON r.id = n.room_id
WHERE n.user_id = $1
  AND n.read_at IS NULL
  AND n.created_at > @after::timestamptz
  AND n.created_at <= @until::timestamptz
GROUP BY n.room_id, r.name, r.kind
ORDER BY latest DESC;

/*
Whether somebody has already been told about this conversation, recently, and
has not yet read it — which is what makes the next message part of a burst
rather than news.
*/
-- name: RecentlyNotifiedInRoom :one
SELECT EXISTS (
    SELECT 1 FROM notifications
    WHERE user_id = $1 AND room_id = $2 AND read_at IS NULL
      AND event_id <> @event_id::text
      AND created_at > now() - make_interval(secs => @window_seconds::double precision)
)::boolean;
