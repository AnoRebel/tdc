-- name: CreateSession :one
INSERT INTO sessions (id, user_id, token_hash, user_agent, ip_address, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT sqlc.embed(sessions), sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = $1
  AND sessions.revoked_at IS NULL
  AND sessions.expires_at > NOW();

-- name: ListSessionsForUser :many
SELECT * FROM sessions
WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
ORDER BY last_seen_at DESC;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = NOW() WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = NOW()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeAllSessionsForUser :exec
UPDATE sessions SET revoked_at = NOW()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: RevokeOtherSessionsForUser :exec
UPDATE sessions SET revoked_at = NOW()
WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions
WHERE expires_at < NOW() - INTERVAL '30 days';

-- name: SetLinkPreviewPreference :exec
UPDATE users SET link_previews_enabled = $2 WHERE id = $1;

-- name: GetLinkPreviewPreference :one
SELECT link_previews_enabled FROM users WHERE id = $1;
