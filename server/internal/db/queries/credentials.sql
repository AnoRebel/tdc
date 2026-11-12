-- name: CreateCredential :one
INSERT INTO credentials (id, user_id, kind, label, secret, external_id, provider, sign_count)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPasswordCredential :one
SELECT * FROM credentials
WHERE user_id = $1 AND kind = 'password';

-- name: GetCredentialByExternalID :one
SELECT * FROM credentials
WHERE kind = $1 AND provider = $2 AND external_id = $3;

-- name: ListCredentialsForUser :many
SELECT * FROM credentials
WHERE user_id = $1
ORDER BY created_at;

-- name: CountCredentialsForUser :one
SELECT COUNT(*) FROM credentials WHERE user_id = $1;

-- name: ReplacePasswordCredential :one
INSERT INTO credentials (id, user_id, kind, label, secret)
VALUES ($1, $2, 'password', '', $3)
ON CONFLICT (user_id) WHERE kind = 'password'
DO UPDATE SET secret = EXCLUDED.secret
RETURNING *;

-- name: TouchCredential :exec
UPDATE credentials
SET last_used_at = NOW(), sign_count = $2
WHERE id = $1;

-- name: DeleteCredential :exec
DELETE FROM credentials WHERE id = $1 AND user_id = $2;
