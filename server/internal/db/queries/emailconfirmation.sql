-- name: CreateEmailConfirmation :one
INSERT INTO email_confirmations (id, token_hash, user_id, address, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Issuing a new one invalidates the old, so somebody who asks twice is not left
-- holding two working links.
-- name: InvalidateEmailConfirmations :execrows
UPDATE email_confirmations
SET used_at = NOW()
WHERE user_id = $1 AND used_at IS NULL;

/*
Spending it.

Unused, unexpired, matched by hash, all in the statement that marks it — so two
simultaneous confirmations produce one success rather than two found by a check
that passed in both.
*/
-- name: SpendEmailConfirmation :one
UPDATE email_confirmations
SET used_at = NOW()
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
RETURNING *;

-- name: DeleteExpiredEmailConfirmations :execrows
DELETE FROM email_confirmations WHERE expires_at <= NOW();

-- Recording an address somebody has asked to prove, which does not touch the
-- one they already have.
-- name: SetPendingEmail :exec
UPDATE users SET pending_email = $2, updated_at = NOW() WHERE id = $1;

/*
Promoting the pending address to the real one.

Conditional on the address still being the one that was asked about: somebody
who changed their mind and asked for a third address must not have a link from
the second complete it.
*/
-- name: ConfirmEmail :one
UPDATE users
SET email = $2,
    email_folded = LOWER($2),
    email_verified = TRUE,
    pending_email = NULL,
    updated_at = NOW()
WHERE id = $1 AND pending_email = $2
RETURNING *;
