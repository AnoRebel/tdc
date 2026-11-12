-- name: CreateReport :one
INSERT INTO reports (
    id, room_id, event_id, subject_address, reporter_id, reporter_address,
    reason, detail, reported_body
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListOpenReports :many
-- The moderation queue: oldest first, so nothing waits indefinitely behind
-- newer arrivals.
SELECT * FROM reports
WHERE status = 'open'
ORDER BY created_at
LIMIT $1;

-- name: ListReportsForRoom :many
SELECT * FROM reports
WHERE room_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: GetReport :one
SELECT * FROM reports WHERE id = $1;

-- name: ResolveReport :one
-- Only an open report can be resolved, so two moderators acting at once do not
-- both record an outcome.
UPDATE reports
SET status = $2, resolved_by = $3, resolution = $4, resolved_at = now()
WHERE id = $1 AND status = 'open'
RETURNING *;

-- name: CountOpenReports :one
SELECT COUNT(*) FROM reports WHERE status = 'open';

-- name: CountRecentReportsBy :one
-- How many reports somebody has filed recently, so a person cannot bury the
-- queue under hundreds of them.
SELECT COUNT(*) FROM reports
WHERE reporter_id = $1 AND created_at > now() - $2::interval;
