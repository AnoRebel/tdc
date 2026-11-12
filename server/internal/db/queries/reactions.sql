-- name: AddReaction :exec
-- Reacting twice with the same emoji is the same as reacting once.
--
-- ON CONFLICT DO NOTHING rather than an error: a double click, or two devices
-- racing, should leave one reaction rather than failing.
INSERT INTO reactions (id, event_id, room_id, user_id, user_address, emoji)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (event_id, user_id, emoji) DO NOTHING;

-- name: RemoveReaction :exec
DELETE FROM reactions
WHERE event_id = $1 AND user_id = $2 AND emoji = $3;

-- name: ListReactionsForEvents :many
-- Every reaction on a set of messages, for drawing a timeline.
--
-- Returned as rows rather than counts so a client can show who reacted and
-- whether the reader is among them, which is what makes a reaction button
-- reflect its own state.
SELECT event_id, emoji, user_id, user_address
FROM reactions
WHERE event_id = ANY(@event_ids::text[])
ORDER BY created_at;

-- name: CountReactionsForEvent :one
SELECT COUNT(*) FROM reactions WHERE event_id = $1;
