-- name: CreateUser :one
INSERT INTO users (id, username, username_folded, display_name, email, email_folded, email_verified)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username_folded = $1;

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
