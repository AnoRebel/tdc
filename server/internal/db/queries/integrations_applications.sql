-- name: CreateApplication :one
INSERT INTO applications (id, name, purpose, requested_permissions, sends_data_externally,
                          external_disclosure, registered_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetApplication :one
SELECT * FROM applications WHERE id = $1;

-- name: GetApplicationByAccount :one
-- The application an account acts for, if it acts for one.
SELECT * FROM applications WHERE account_id = $1;

-- name: ListApplications :many
SELECT * FROM applications WHERE state <> 'removed' ORDER BY registered_at DESC;

-- name: ListApprovedApplications :many
SELECT * FROM applications WHERE state = 'approved' ORDER BY approved_at DESC;

-- name: ApproveApplication :one
/*
Approval copies the request into the grant, and only while it is still the
request the approver was shown: an application whose request changed between
being read and being approved is refused rather than granted the new one.
*/
UPDATE applications
SET state = 'approved', granted_permissions = requested_permissions,
    approved_by = $2, approved_at = now(), account_id = $3
WHERE id = $1 AND state = 'registered' AND requested_permissions = sqlc.arg(shown)::text[]
RETURNING *;

-- name: RemoveApplication :one
UPDATE applications
SET state = 'removed', removed_by = $2, removed_at = now()
WHERE id = $1 AND state <> 'removed'
RETURNING *;

-- name: RecordApplicationRateLimited :exec
UPDATE applications
SET rate_limited_count = rate_limited_count + 1, last_rate_limited_at = now()
WHERE id = $1;

-- name: SetApplicationChannel :exec
INSERT INTO application_channels (application_id, room_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ClearApplicationChannels :exec
DELETE FROM application_channels WHERE application_id = $1;

-- name: ListApplicationChannels :many
SELECT room_id FROM application_channels WHERE application_id = $1;

-- name: ListApplicationsInRoom :many
-- The approved applications that may act in a channel, which is what that
-- channel's members are entitled to know.
SELECT a.* FROM applications AS a
JOIN application_channels AS c ON c.application_id = a.id
WHERE c.room_id = $1 AND a.state = 'approved'
ORDER BY a.name;

-- name: CreateApplicationToken :one
INSERT INTO api_tokens (id, user_id, label, token_hash, scope, application_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, label, created_at;

-- name: ListApplicationTokens :many
SELECT id, label, last_used_at, created_at FROM api_tokens
WHERE application_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: RevokeApplicationToken :one
UPDATE api_tokens SET revoked_at = now()
WHERE id = $1 AND application_id = $2 AND revoked_at IS NULL
RETURNING id, label;

-- name: RevokeApplicationTokens :exec
UPDATE api_tokens SET revoked_at = now()
WHERE application_id = $1 AND revoked_at IS NULL;

-- name: ListEventIDsBySender :many
-- What an account posted, for removing it with the application it acted for.
SELECT id, room_id FROM events
WHERE sender_id = $1 AND type = 'message.sent' AND redacted_at IS NULL;
