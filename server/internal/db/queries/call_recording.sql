-- name: StartCallRecording :execrows
-- Marks somebody in a call as recording it, unless they already are.
UPDATE call_participants SET recording_since = NOW()
WHERE call_id = $1 AND user_id = $2 AND left_at IS NULL AND recording_since IS NULL;

-- name: UndoCallRecording :exec
-- Takes back a recording that could not be recorded in the log, so nobody is
-- shown as recording what was never announced.
UPDATE call_participants SET recording_since = NULL WHERE call_id = $1 AND user_id = $2;

-- name: StopCallRecording :one
-- Stops somebody's recording, returning when it began. The joined copy of the
-- row is read before the update, which is how the old value comes back.
UPDATE call_participants p SET recording_since = NULL
FROM call_participants o
WHERE o.call_id = p.call_id AND o.user_id = p.user_id
  AND p.call_id = $1 AND p.user_id = $2 AND p.recording_since IS NOT NULL
RETURNING o.recording_since::timestamptz AS began;

-- name: StopAbsentRecordings :many
-- Stops the recordings of people no longer in their call — they left, their
-- connection dropped, or the call ended — returning each with when it began.
UPDATE call_participants p SET recording_since = NULL
FROM call_participants o, calls c
WHERE o.call_id = p.call_id AND o.user_id = p.user_id AND c.id = p.call_id
  AND p.recording_since IS NOT NULL AND (p.left_at IS NOT NULL OR c.ended_at IS NOT NULL)
RETURNING p.call_id, p.user_id, c.room_id, o.recording_since::timestamptz AS began;
