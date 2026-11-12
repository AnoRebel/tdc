-- name: CreateDirectTransfer :one
INSERT INTO direct_transfers (id, room_id, sender_id, recipient_id, filename, size_bytes, content_type, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetDirectTransfer :one
SELECT * FROM direct_transfers WHERE id = $1;

-- Moves an offer from one state to another, only if it is still in one of the
-- states it may move from and has not lapsed — so two people acting at once
-- cannot both win, and an offer nobody answered cannot be accepted late.
-- name: MoveDirectTransfer :one
UPDATE direct_transfers
SET state = @to_state::text,
    expires_at = COALESCE(sqlc.narg(expires_at), expires_at),
    ended_at = CASE WHEN @to_state::text IN ('offered', 'accepted') THEN NULL ELSE now() END
WHERE id = @id
  AND state = ANY(@from_states::text[])
  AND expires_at > now()
RETURNING *;

-- Offers and transfers whose time ran out.
-- name: ExpireDirectTransfers :many
UPDATE direct_transfers
SET state = 'expired', ended_at = now()
WHERE state IN ('offered', 'accepted') AND expires_at <= now()
RETURNING *;

-- name: DeleteEndedDirectTransfers :exec
DELETE FROM direct_transfers WHERE ended_at IS NOT NULL AND ended_at < $1;
