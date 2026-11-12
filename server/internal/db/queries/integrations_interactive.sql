-- name: GetMessageInteraction :one
-- A message's controls as its program last left them. No row means the
-- message is as it was posted.
SELECT * FROM message_interactions WHERE event_id = $1;

-- name: CreateMessageInteraction :one
-- The first update to a message, from version zero to one.
--
-- ON CONFLICT DO NOTHING is the compare-and-swap for the first step: two
-- updates both expecting version zero both try to insert, and exactly one
-- row is written. The loser gets no row back and is told it was stale.
INSERT INTO message_interactions (event_id, room_id, version, body, actions, updated_by)
VALUES ($1, $2, 1, $3, $4, $5)
ON CONFLICT (event_id) DO NOTHING
RETURNING *;

-- name: AdvanceMessageInteraction :one
-- Every later update, which succeeds only against the version it names.
UPDATE message_interactions
SET version = version + 1,
    body = $3,
    actions = $4,
    updated_by = $5,
    updated_at = now()
WHERE event_id = $1 AND version = $2
RETURNING *;

-- name: GetIntegrationContext :one
-- What one program remembers about one person in one conversation.
SELECT * FROM integration_context
WHERE integration_id = $1 AND room_id = $2 AND user_id = $3;

-- name: PutIntegrationContext :one
INSERT INTO integration_context (integration_id, room_id, user_id, state)
VALUES ($1, $2, $3, $4)
ON CONFLICT (integration_id, room_id, user_id)
DO UPDATE SET state = EXCLUDED.state, updated_at = now()
RETURNING *;

-- name: DeleteIntegrationContext :execrows
DELETE FROM integration_context
WHERE integration_id = $1 AND room_id = $2 AND user_id = $3;

-- name: DeleteIntegrationContextForIntegration :execrows
-- Everything a program remembered, for when it is removed.
DELETE FROM integration_context WHERE integration_id = $1;
