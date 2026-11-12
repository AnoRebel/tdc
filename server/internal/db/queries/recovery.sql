-- name: CreateRecoveryToken :one
INSERT INTO recovery_tokens (id, token_hash, user_id, sent_to, requested_from, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

/*
Every outstanding token for an account, spent at once.

Issuing a new one invalidates the old, which the specification requires: a
person who asks twice because the first mail was slow must not be left with two
working links, and somebody who asks again after a suspicious message must be
able to make the earlier one stop working.
*/
-- name: InvalidateRecoveryTokens :execrows
UPDATE recovery_tokens
SET used_at = NOW()
WHERE user_id = $1 AND used_at IS NULL;

-- Read without spending, so the client can tell somebody their link has expired
-- before asking them to type a new password into a form that cannot work.
-- name: GetRecoveryToken :one
SELECT * FROM recovery_tokens
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW();

/*
Spending it.

The conditions are the rule: unused, unexpired, and matched by hash in the same
statement that marks it. Two simultaneous redemptions produce one success and
one no-op rather than two successes found by a check that passed in both.
*/
-- name: SpendRecoveryToken :one
UPDATE recovery_tokens
SET used_at = NOW()
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
RETURNING *;

-- name: DeleteExpiredRecoveryTokens :execrows
DELETE FROM recovery_tokens WHERE expires_at <= NOW();
