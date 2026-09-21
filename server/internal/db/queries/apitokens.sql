-- name: CreateAPIToken :one
INSERT INTO api_tokens (id, user_id, label, token_hash, scope, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAPITokenByHash :one
-- The per-request lookup, joined to the account so one query answers both
-- "is this token good" and "who is it".
--
-- Expiry and revocation are filtered here rather than checked in Go: a token
-- that has stopped working should be indistinguishable from one that never
-- existed, and doing it in the query means there is no window where a caller
-- reads the row and forgets to check.
SELECT sqlc.embed(api_tokens), sqlc.embed(users)
FROM api_tokens
JOIN users ON users.id = api_tokens.user_id
WHERE api_tokens.token_hash = $1
  AND api_tokens.revoked_at IS NULL
  AND (api_tokens.expires_at IS NULL OR api_tokens.expires_at > NOW());

-- name: TouchAPIToken :exec
-- When it was last used, for the list where somebody decides what to revoke.
--
-- Rounded to the minute by the caller rather than written on every request:
-- an exact timestamp would mean a write per request for no benefit anybody can
-- see in a list that shows "last used yesterday".
UPDATE api_tokens SET last_used_at = $2 WHERE id = $1;

-- name: ListAPITokensForUser :many
-- Never the hash. What this returns is what a person is shown, and the token
-- itself is not recoverable by anybody, including its owner.
SELECT id, user_id, label, scope, expires_at, revoked_at, last_used_at, created_at
FROM api_tokens
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: RevokeAPIToken :one
-- Scoped to the owner, so somebody cannot revoke a token by guessing an
-- identifier. Returns the row so the caller can name it in the audit entry
-- and can tell "revoked" from "was not yours".
UPDATE api_tokens
SET revoked_at = NOW()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
RETURNING id, label;

-- name: RevokeAPITokensForUser :exec
-- Every token an account holds, for a suspension or a password change: a
-- credential that outlived the account's own access would be the way back in.
UPDATE api_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL;
