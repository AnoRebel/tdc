/*
Verifications of claims on profiles.
*/

-- name: InsertVerification :one
INSERT INTO profile_verifications (
    id, user_id, claim_kind, claim_id, claim_digest, claim_snapshot,
    note, verified_by, verified_by_label
) VALUES (
    @id, @user_id, @claim_kind, @claim_id, @claim_digest, @claim_snapshot,
    @note, @verified_by, @verified_by_label
)
RETURNING *;

-- name: StandingVerifications :many
-- Every verification on a profile that has been neither revoked nor cleared.
-- Whether each still matches its claim is decided by the caller, which holds
-- the claim as it stands.
SELECT * FROM profile_verifications
WHERE user_id = $1 AND revoked_at IS NULL AND cleared_at IS NULL
ORDER BY verified_at;

-- name: GetVerification :one
SELECT * FROM profile_verifications WHERE id = $1;

-- name: RevokeVerification :execrows
UPDATE profile_verifications
SET revoked_at = NOW(), revoked_by = @revoked_by, revoke_reason = @reason
WHERE id = @id AND revoked_at IS NULL AND cleared_at IS NULL;

-- name: ClearVerification :execrows
UPDATE profile_verifications SET cleared_at = NOW()
WHERE id = $1 AND revoked_at IS NULL AND cleared_at IS NULL;

-- name: GetOwnCompanyClaims :many
-- The company associations a profile can have verified: accepted ones.
SELECT c.id, c.slug, c.name, m.role
FROM company_members m
JOIN company_profiles c ON c.id = m.company_id
WHERE m.user_id = $1 AND m.status = 'accepted'
ORDER BY m.accepted_at;
