-- name: CreateUser :one
INSERT INTO users (id, username, username_folded, display_name, email, email_folded, email_verified)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- Whoever a name means: its holder, or the account that most recently gave it
-- up, so an old link or a saved filter still finds the person it named — and
-- somebody whose desktop application remembers the name they used to have can
-- still sign in with it, for as long as nobody else has taken it.
-- name: GetUserByUsername :one
SELECT * FROM users WHERE id = user_named($1);

-- Only the account holding the name now: whether a name is free to take.
-- name: GetUserByCurrentUsername :one
SELECT * FROM users WHERE username_folded = $1;

-- Serialises everything that hands out or gives up one name, for the length
-- of a transaction: a rename away from a name and somebody else claiming it
-- would otherwise each see the other's state from before.
-- name: LockUsername :exec
SELECT pg_advisory_xact_lock(hashtext('username:' || @folded::text));

-- name: RenameUser :one
UPDATE users
SET username        = @username,
    username_folded = @username_folded,
    updated_at      = NOW()
WHERE id = @id
RETURNING *;

-- name: RecordFormerUsername :exec
INSERT INTO username_history (user_id, username, username_folded)
VALUES (@user_id, @username, @username_folded);

-- When this account last changed its username, if it ever has.
-- name: LastUsernameChange :one
SELECT changed_at FROM username_history
WHERE user_id = $1
ORDER BY changed_at DESC
LIMIT 1;

-- Whether somebody else gave this name up recently enough that it is still
-- theirs to come back to.
-- name: UsernameHeldByOther :one
SELECT EXISTS (
    SELECT 1 FROM username_history
    WHERE username_folded = @folded
      AND user_id <> @user_id
      AND changed_at > @since
)::boolean;

-- name: ListFormerUsernames :many
SELECT username, changed_at FROM username_history
WHERE user_id = $1
ORDER BY changed_at DESC;

-- Former names for a page of accounts at once, for the administrators' list.
-- name: ListFormerUsernamesFor :many
SELECT user_id, username, changed_at FROM username_history
WHERE user_id = ANY(@user_ids::uuid[])
ORDER BY changed_at DESC;

/*
Whether this person is in any conversation another server takes part in: one
hosted elsewhere, or one hosted here that has peers.

Other servers know a person by their address and nothing else — it is in the
signed events they hold and the membership they keep — and the protocol has no
way to say "this address is now that one". A rename would leave them a member
under a name that no longer exists here.
*/
-- name: InFederatedRoom :one
SELECT EXISTS (
    SELECT 1 FROM room_members m
    JOIN rooms r ON r.id = m.room_id
    WHERE m.user_id = @user_id
      AND (lower(r.home_server) <> lower(@server_name::text)
           OR EXISTS (SELECT 1 FROM room_peers p WHERE p.room_id = r.id))
)::boolean;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email_folded = $1;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = $2,
    updated_at   = NOW()
WHERE id = $1
RETURNING *;

-- name: SuspendUser :exec
UPDATE users
SET suspended_at    = NOW(),
    suspended_until = $2,
    suspended_by    = $3,
    suspension_note = $4,
    updated_at      = NOW()
WHERE id = $1;

-- name: LiftUserSuspension :exec
UPDATE users
SET suspended_at    = NULL,
    suspended_until = NULL,
    suspended_by    = NULL,
    suspension_note = NULL,
    updated_at      = NOW()
WHERE id = $1;

-- What this person chose the interface to look like.
--
-- Empty in any column means they have chosen nothing there and take the
-- operator's default, which is resolved by the client rather than here: the
-- server reports what was chosen, and choosing nothing is an answer.
-- name: GetAppearance :one
SELECT appearance_palette, appearance_mode, appearance_accent
FROM users WHERE id = $1;

-- name: SetAppearance :exec
UPDATE users
SET appearance_palette = $2, appearance_mode = $3, appearance_accent = $4
WHERE id = $1;

-- Which introductions this person has already been given.
--
-- Read whole and written whole: the structure is small, and a partial update
-- would mean the database knowing what a tour is.
-- name: GetTourProgress :one
SELECT tour_progress FROM users WHERE id = $1;

-- name: SetTourProgress :exec
UPDATE users SET tour_progress = $2 WHERE id = $1;
