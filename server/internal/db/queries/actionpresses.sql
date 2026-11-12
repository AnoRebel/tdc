-- name: RecordActionPress :one
-- The press, recorded before it is announced.
--
-- ON CONFLICT DO NOTHING rather than an upsert: a second press of the same
-- button by the same person is not a change, it is a repeat, and the caller
-- needs to tell the two apart. No row returned means somebody pressed twice.
--
-- The version is part of what makes it the same button: a program that
-- replaced a message's controls has asked a new question, even under an old
-- name.
INSERT INTO action_presses (id, event_id, room_id, action, user_id, user_address, version, value)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (event_id, user_id, action, version) DO NOTHING
RETURNING *;

-- name: CountActionPresses :one
-- How many times a button has been pressed by anybody, for a single-use
-- action, on the version of the controls that offered it.
SELECT COUNT(*) FROM action_presses WHERE event_id = $1 AND action = $2 AND version = $3;

-- name: ListActionPressesForEvent :many
-- Who pressed what on one message, oldest first.
SELECT * FROM action_presses WHERE event_id = $1 ORDER BY created_at;
