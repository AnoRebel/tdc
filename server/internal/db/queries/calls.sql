-- name: OpenCallInRoom :one
SELECT * FROM calls WHERE room_id = $1 AND ended_at IS NULL;

-- name: StartCall :one
-- ON CONFLICT so two people starting at once both end up in one call: the
-- second insert finds the first's row instead of failing.
INSERT INTO calls (id, room_id, started_by) VALUES ($1, $2, $3)
ON CONFLICT (room_id) WHERE ended_at IS NULL DO NOTHING
RETURNING *;

-- name: JoinCall :exec
INSERT INTO call_participants (call_id, user_id, audio, video)
VALUES ($1, $2, $3, $4)
ON CONFLICT (call_id, user_id)
DO UPDATE SET left_at = NULL, joined_at = NOW(), audio = EXCLUDED.audio, video = EXCLUDED.video,
              held = FALSE;

-- name: LeaveCall :execrows
UPDATE call_participants SET left_at = NOW()
WHERE call_id = $1 AND user_id = $2 AND left_at IS NULL;

-- name: SetCallMedia :execrows
UPDATE call_participants SET audio = $3, video = $4, held = $5
WHERE call_id = $1 AND user_id = $2 AND left_at IS NULL;

-- name: ListCallParticipants :many
SELECT p.user_id, p.joined_at, p.audio, p.video, p.held, u.username
FROM call_participants p
JOIN users u ON u.id = p.user_id
WHERE p.call_id = $1 AND p.left_at IS NULL
ORDER BY p.joined_at;

-- name: CallRoomOfParticipant :one
-- The room of an open call somebody is in now; no row if they are not in it.
SELECT c.room_id FROM call_participants p JOIN calls c ON c.id = p.call_id
WHERE p.call_id = $1 AND p.user_id = $2 AND p.left_at IS NULL AND c.ended_at IS NULL;

-- name: EndCallIfEmpty :one
-- Ends a call nobody is in any more, returning it if this ended it.
UPDATE calls SET ended_at = NOW()
WHERE id = $1 AND ended_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM call_participants WHERE call_id = $1 AND left_at IS NULL)
RETURNING *;

-- name: LeaveCallsOfDisconnected :many
-- Takes out of their calls everybody with no live connection left, so a
-- closed laptop or a lost network does not leave somebody "in" a call forever.
UPDATE call_participants p SET left_at = NOW()
FROM calls c
WHERE c.id = p.call_id AND c.ended_at IS NULL AND p.left_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM user_presence up WHERE up.user_id = p.user_id AND up.connections > 0)
RETURNING p.call_id, p.user_id, c.room_id;

-- name: SetCallStartedEvent :exec
UPDATE calls SET started_event = $2 WHERE id = $1;

-- name: CallByID :one
SELECT * FROM calls WHERE id = $1;

-- name: InviteToCall :exec
INSERT INTO call_invites (call_id, user_id, invited_by) VALUES ($1, $2, $3)
ON CONFLICT (call_id, user_id) DO NOTHING;

-- name: IsInvitedToCall :one
SELECT EXISTS (SELECT 1 FROM call_invites WHERE call_id = $1 AND user_id = $2);

-- name: ShareAnOpenCall :one
-- Whether two people are in the same open call now.
SELECT EXISTS (
    SELECT 1 FROM call_participants a
    JOIN call_participants b ON b.call_id = a.call_id
    JOIN calls c ON c.id = a.call_id
    WHERE a.user_id = $1 AND b.user_id = $2
      AND a.left_at IS NULL AND b.left_at IS NULL AND c.ended_at IS NULL);
