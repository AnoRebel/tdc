-- name: StartOAuthAuthorization :one
INSERT INTO oauth_authorizations (id, state_hash, verifier, provider, redirect_to, user_id, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- Taking the authorization consumes it: the row is deleted as it is read, so a
-- state value cannot be presented twice even if two callbacks arrive at once.
-- name: TakeOAuthAuthorization :one
DELETE FROM oauth_authorizations
WHERE state_hash = $1 AND expires_at > NOW()
RETURNING *;

-- name: DeleteExpiredOAuthAuthorizations :execrows
DELETE FROM oauth_authorizations WHERE expires_at <= NOW();

-- name: CreateOAuthCredential :one
INSERT INTO credentials (
    id, user_id, kind, label, external_id, provider,
    external_email, external_email_verified
)
VALUES ($1, $2, 'oauth2', $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetOAuthCredential :one
SELECT * FROM credentials
WHERE kind = 'oauth2' AND provider = $1 AND external_id = $2;

-- name: ListOAuthCredentialsForUser :many
SELECT * FROM credentials
WHERE user_id = $1 AND kind = 'oauth2'
ORDER BY created_at;

-- The provider's claim about the account may change between sign-ins, so it is
-- refreshed rather than left at whatever it was the first time.
-- name: UpdateOAuthCredentialEmail :exec
UPDATE credentials
SET external_email = $2, external_email_verified = $3, last_used_at = NOW()
WHERE id = $1 AND kind = 'oauth2';

-- name: ParkOAuthOutcome :one
INSERT INTO oauth_outcomes (id, handle_hash, status, detail, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Collected once: the row is deleted as it is read, so a handle presented twice
-- yields nothing the second time.
-- name: CollectOAuthOutcome :one
DELETE FROM oauth_outcomes
WHERE handle_hash = $1 AND expires_at > NOW()
RETURNING *;

-- name: DeleteExpiredOAuthOutcomes :execrows
DELETE FROM oauth_outcomes WHERE expires_at <= NOW();
