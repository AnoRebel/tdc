-- name: CreateTOTPCredential :one
INSERT INTO credentials (id, user_id, kind, label, secret)
VALUES ($1, $2, 'totp', $3, $4)
RETURNING *;

-- name: GetTOTPCredential :one
SELECT * FROM credentials
WHERE user_id = $1 AND kind = 'totp';

-- Confirming enrolment: the person proved their app holds the secret. The
-- period is recorded at the same moment, so the code that confirmed it cannot
-- then be replayed to sign in.
-- name: ConfirmTOTPCredential :exec
UPDATE credentials
SET confirmed_at = NOW(), last_period = $2, last_used_at = NOW()
WHERE id = $1 AND kind = 'totp';

/*
Accepting a code, and refusing one from a period already used.

The WHERE clause is the single-use rule: an update that matches no row means
the code belonged to a period at or before the last accepted one, which is a
replay. Doing it as a condition rather than a read-then-write means two
simultaneous attempts cannot both succeed.
*/
-- name: AdvanceTOTPPeriod :one
UPDATE credentials
SET last_period = $2, last_used_at = NOW()
WHERE id = $1
  AND kind = 'totp'
  AND (last_period IS NULL OR last_period < $2)
RETURNING *;

-- name: DeleteTOTPCredential :exec
DELETE FROM credentials WHERE user_id = $1 AND kind = 'totp';

-- name: CreateRecoveryCode :one
INSERT INTO credentials (id, user_id, kind, label, secret, confirmed_at)
VALUES ($1, $2, 'recovery', $3, $4, NOW())
RETURNING *;

-- name: ListRecoveryCodes :many
SELECT * FROM credentials
WHERE user_id = $1 AND kind = 'recovery'
ORDER BY created_at;

-- name: CountUnusedRecoveryCodes :one
SELECT COUNT(*) FROM credentials
WHERE user_id = $1 AND kind = 'recovery' AND used_at IS NULL;

/*
Spending a recovery code.

Matched on the hash and required to be unused, so the update itself is what
makes it single-use: two attempts with the same code produce one success and
one no-op, rather than two successes found by a check that passed in both.
*/
-- name: SpendRecoveryCode :one
UPDATE credentials
SET used_at = NOW(), last_used_at = NOW()
WHERE user_id = $1
  AND kind = 'recovery'
  AND secret = $2
  AND used_at IS NULL
RETURNING *;

-- Replacing a batch: the old codes go when new ones are issued, since a person
-- holding a printed list expects that list to be the whole truth.
-- name: DeleteRecoveryCodes :exec
DELETE FROM credentials WHERE user_id = $1 AND kind = 'recovery';

-- name: StartPendingSignIn :one
INSERT INTO pending_sign_ins (id, ticket_hash, user_id, user_agent, ip_address, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- Read without consuming: a wrong code must leave the ticket usable, or one
-- mistyped digit would mean signing in from the beginning.
-- name: GetPendingSignIn :one
SELECT * FROM pending_sign_ins
WHERE ticket_hash = $1 AND expires_at > NOW();

-- name: CountPendingAttempt :one
UPDATE pending_sign_ins
SET attempts = attempts + 1
WHERE id = $1
RETURNING *;

-- Consumed when the second factor is accepted, so a ticket buys one session.
-- name: FinishPendingSignIn :one
DELETE FROM pending_sign_ins
WHERE ticket_hash = $1 AND expires_at > NOW()
RETURNING *;

-- name: DeleteExpiredPendingSignIns :execrows
DELETE FROM pending_sign_ins WHERE expires_at <= NOW();

/*
Whether an account has a second factor to be asked for at all.

A passkey counts. Somebody who already carries one has the thing a second factor
is for — a device in their possession — and asking them to carry an authenticator
app as well is ceremony rather than security. It is also the difference between
somebody turning two-step sign-in on and somebody deciding it is not worth the
trouble.

Confirmed only, for TOTP: a secret somebody started enrolling and abandoned must
not lock them out. A passkey has no equivalent half-finished state — it exists
once the authenticator has made it.
*/
-- name: HasConfirmedSecondFactor :one
SELECT EXISTS (
    SELECT 1 FROM credentials
    WHERE user_id = $1
      AND (
        (kind = 'totp' AND confirmed_at IS NOT NULL)
        OR kind = 'passkey'
      )
) AS has_second_factor;

-- The passkeys an account could answer a second-factor challenge with.
-- name: HasPasskey :one
SELECT EXISTS (
    SELECT 1 FROM credentials WHERE user_id = $1 AND kind = 'passkey'
) AS has_passkey;

-- Recording a code that has just been mailed. Replaces any outstanding one, so
-- asking again is how somebody who lost the first message gets a usable code.
-- name: SetEmailedCode :exec
UPDATE pending_sign_ins
SET emailed_code_hash = $2, emailed_code_expires_at = $3
WHERE id = $1;

/*
Spending an emailed code.

Matched, unexpired, and cleared in the statement that checks it — so the code is
good once even if two attempts arrive together, and a wrong guess cannot be told
from an expired one.
*/
-- name: SpendEmailedCode :one
UPDATE pending_sign_ins
SET emailed_code_hash = NULL, emailed_code_expires_at = NULL
WHERE ticket_hash = $1
  AND emailed_code_hash = $2
  AND emailed_code_expires_at > NOW()
  AND expires_at > NOW()
RETURNING *;
