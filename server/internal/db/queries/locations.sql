/*
Somebody's own location, for the settings screen.

Reads of other people's go through person_location() inside the query that
returns the person, never through these.
*/

-- name: GetOwnLocation :one
SELECT place, mode, visibility, detected_at, updated_at
FROM user_locations WHERE user_id = $1;

-- name: SetOwnLocation :one
-- The whole setting at once. detected_at is kept unless a detection is being
-- recorded, so switching to manual does not forget when the place was found.
INSERT INTO user_locations (user_id, place, mode, visibility, detected_at, updated_at)
VALUES (@user_id, @place, @mode, @visibility, sqlc.narg('detected_at'), NOW())
ON CONFLICT (user_id) DO UPDATE SET
    place = EXCLUDED.place,
    mode = EXCLUDED.mode,
    visibility = EXCLUDED.visibility,
    detected_at = COALESCE(EXCLUDED.detected_at, user_locations.detected_at),
    updated_at = NOW()
RETURNING place, mode, visibility, detected_at, updated_at;

-- name: ModerateLocation :exec
-- A moderator replacing what somebody's location says, from the directory's
-- moderation screen. Made manual, so a detection does not quietly undo it.
UPDATE user_locations
SET place = @place,
    mode = CASE WHEN place = @place THEN mode ELSE 'manual' END,
    updated_at = NOW()
WHERE user_id = @user_id;
