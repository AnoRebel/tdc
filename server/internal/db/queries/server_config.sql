-- name: GetServerConfigValue :one
SELECT key, value, is_secret, updated_at, updated_by
FROM server_config
WHERE key = $1;

-- name: ListServerConfig :many
SELECT key, value, is_secret, updated_at, updated_by
FROM server_config
ORDER BY key;

-- name: SetServerConfigValue :exec
INSERT INTO server_config (key, value, is_secret, updated_at, updated_by)
VALUES ($1, $2, $3, NOW(), $4)
ON CONFLICT (key) DO UPDATE
SET value      = EXCLUDED.value,
    is_secret  = EXCLUDED.is_secret,
    updated_at = NOW(),
    updated_by = EXCLUDED.updated_by;

-- name: DeleteServerConfigValue :exec
DELETE FROM server_config WHERE key = $1;
