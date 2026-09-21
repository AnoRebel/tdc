-- name: RecordActionPress :one
-- The press, recorded before it is announced.
--
-- ON CONFLICT DO NOTHING rather than an upsert: a second press of the same
-- button by the same person is not a change, it is a repeat, and the caller
-- needs to tell the two apart. No row returned means somebody pressed twice.
INSERT INTO action_presses (id, event_id, room_id, action, user_id, user_address)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (event_id, user_id, action) DO NOTHING
RETURNING *;

-- name: CountActionPresses :one
-- How many times a button has been pressed by anybody, for a single-use
-- action.
SELECT COUNT(*) FROM action_presses WHERE event_id = $1 AND action = $2;

-- name: ListActionPressesForEvent :many
-- Who pressed what on one message, oldest first.
SELECT * FROM action_presses WHERE event_id = $1 ORDER BY created_at;
