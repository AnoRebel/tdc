-- name: CreatePasskeyCredential :one
INSERT INTO credentials (id, user_id, kind, label, secret, external_id, sign_count, passkey)
VALUES ($1, $2, 'passkey', $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPasskeyByCredentialID :one
SELECT * FROM credentials
WHERE kind = 'passkey' AND external_id = $1;

-- name: ListPasskeysForUser :many
SELECT * FROM credentials
WHERE user_id = $1 AND kind = 'passkey'
ORDER BY created_at;

-- name: RenamePasskey :one
UPDATE credentials
SET label = $3
WHERE id = $1 AND user_id = $2 AND kind = 'passkey'
RETURNING *;

-- Establishing the handle when somebody enrols their first passkey. Only ever
-- set once: it is what the authenticator remembers, and changing it would
-- orphan every passkey already enrolled.
-- name: EnsureWebauthnHandle :one
UPDATE users
SET webauthn_handle = COALESCE(webauthn_handle, $2)
WHERE id = $1
RETURNING webauthn_handle;

-- name: GetUserByWebauthnHandle :one
SELECT * FROM users WHERE webauthn_handle = $1;

-- name: StartWebauthnCeremony :one
INSERT INTO webauthn_ceremonies (id, challenge_hash, session, purpose, user_id, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- Taking it consumes it, so a challenge cannot be answered twice even if two
-- responses arrive at once.
-- name: TakeWebauthnCeremony :one
DELETE FROM webauthn_ceremonies
WHERE challenge_hash = $1 AND purpose = $2 AND expires_at > NOW()
RETURNING *;

-- name: DeleteExpiredWebauthnCeremonies :execrows
DELETE FROM webauthn_ceremonies WHERE expires_at <= NOW();
