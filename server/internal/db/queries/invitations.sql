-- name: CreateInvitation :one
INSERT INTO invitations (
    id, token_hash, code_hash, room_id, created_by, created_by_address,
    bound_to, max_uses, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: FindInvitationByTokenHash :one
SELECT * FROM invitations WHERE token_hash = $1;

-- name: FindInvitationByCodeHash :one
SELECT * FROM invitations WHERE code_hash = $1;

-- name: ListInvitationsForRoom :many
-- What a channel has outstanding, for whoever manages it.
--
-- Includes revoked and expired ones: somebody auditing what was issued needs
-- to see what has been withdrawn, not only what still works.
SELECT * FROM invitations
WHERE room_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListServerInvitations :many
SELECT * FROM invitations
WHERE room_id IS NULL
ORDER BY created_at DESC
LIMIT $1;

-- name: RevokeInvitation :one
-- Revoking is idempotent in effect but recorded once: a second revocation
-- leaves the first one's timestamp, which is when it actually stopped working.
UPDATE invitations
SET revoked_at = COALESCE(revoked_at, now()), revoked_by = COALESCE(revoked_by, $2)
WHERE id = $1
RETURNING *;

-- name: RecordInvitationUse :execrows
-- Claims one use, atomically.
--
-- The count is incremented in the same statement that checks it, so two people
-- accepting the last use of an invitation cannot both succeed. A check
-- followed by an update would be a race with a real consequence: an invitation
-- limited to one use admitting two people.
UPDATE invitations
SET uses = uses + 1
WHERE id = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
  AND (max_uses IS NULL OR uses < max_uses);

-- name: RecordInvitationUser :exec
-- Who accepted, for the audit trail. ON CONFLICT DO NOTHING because somebody
-- accepting twice is one person, not two.
INSERT INTO invitation_uses (invitation_id, user_id)
VALUES ($1, $2)
ON CONFLICT (invitation_id, user_id) DO NOTHING;

-- name: HasUsedInvitation :one
SELECT EXISTS (
    SELECT 1 FROM invitation_uses WHERE invitation_id = $1 AND user_id = $2
);

-- name: ListInvitationUsers :many
-- Who joined through one invitation, newest first.
SELECT u.id, u.username, iu.used_at
FROM invitation_uses iu
JOIN users u ON u.id = iu.user_id
WHERE iu.invitation_id = $1
ORDER BY iu.used_at DESC;
