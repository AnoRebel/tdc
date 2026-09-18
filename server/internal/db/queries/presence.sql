-- name: ConnectionOpened :one
-- Records a connection, marking the person present.
--
-- Counted rather than set to true: presence ends when the last connection
-- closes, not the first, so someone on a phone and a laptop does not appear
-- offline when they close one.
INSERT INTO user_presence (user_id, connections, last_seen_at, updated_at)
VALUES ($1, 1, NOW(), NOW())
ON CONFLICT (user_id) DO UPDATE
SET connections  = user_presence.connections + 1,
    last_seen_at = NOW(),
    updated_at   = NOW()
RETURNING *;

-- name: ConnectionClosed :exec
-- GREATEST keeps the count from going negative if a close is somehow counted
-- twice, which would otherwise leave someone permanently "present".
UPDATE user_presence
SET connections  = GREATEST(connections - 1, 0),
    last_seen_at = NOW(),
    updated_at   = NOW()
WHERE user_id = $1;

-- name: TouchPresence :exec
UPDATE user_presence SET last_seen_at = NOW() WHERE user_id = $1;

-- name: GetPresence :one
SELECT * FROM user_presence WHERE user_id = $1;

-- name: ListPresenceFor :many
SELECT * FROM user_presence WHERE user_id = ANY($1::uuid[]);

-- name: SetStatus :one
INSERT INTO user_presence (user_id, status, status_text, status_expires_at, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (user_id) DO UPDATE
SET status            = EXCLUDED.status,
    status_text       = EXCLUDED.status_text,
    status_expires_at = EXCLUDED.status_expires_at,
    updated_at        = NOW()
RETURNING *;

-- name: SetInvisible :exec
INSERT INTO user_presence (user_id, invisible, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE
SET invisible = EXCLUDED.invisible, updated_at = NOW();

-- name: ClearExpiredStatuses :exec
-- A status with an expiry reverts on its own, so a temporary one needs no
-- later action from the person who set it.
UPDATE user_presence
SET status            = 'available',
    status_text       = '',
    status_expires_at = NULL,
    updated_at        = NOW()
WHERE status_expires_at IS NOT NULL AND status_expires_at <= NOW();

-- name: ResetStalePresence :exec
-- Clears presence for connections that ended without a clean close, which is
-- what makes an ungraceful disconnect expire rather than persist.
UPDATE user_presence
SET connections = 0, updated_at = NOW()
WHERE connections > 0 AND last_seen_at < $1;

-- name: ListServerStatuses :many
SELECT * FROM server_statuses ORDER BY sort_order, label;

-- name: GetServerStatus :one
SELECT * FROM server_statuses WHERE key = $1;

-- name: StartTyping :exec
INSERT INTO typing_indicators (room_id, user_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (room_id, user_id) DO UPDATE SET expires_at = EXCLUDED.expires_at;

-- name: StopTyping :exec
DELETE FROM typing_indicators WHERE room_id = $1 AND user_id = $2;

-- name: ListTyping :many
SELECT typing_indicators.user_id, users.username, users.display_name
FROM typing_indicators
JOIN users ON users.id = typing_indicators.user_id
WHERE typing_indicators.room_id = $1 AND typing_indicators.expires_at > NOW();

-- name: ClearExpiredTyping :exec
DELETE FROM typing_indicators WHERE expires_at <= NOW();

-- name: SetTypingPreference :exec
UPDATE users SET sends_typing_indication = $2, updated_at = NOW() WHERE id = $1;

-- name: GetDisclosurePreferences :one
-- What this person lets a member list say about them.
--
-- Defaults when there is no row, which is the ordinary case: user_privacy is
-- written on the first change, and somebody who has never opened these
-- settings discloses all three.
SELECT
    COALESCE(shows_presence, TRUE)::boolean  AS shows_presence,
    COALESCE(shows_status, TRUE)::boolean    AS shows_status,
    COALESCE(shows_last_seen, TRUE)::boolean AS shows_last_seen
FROM user_privacy WHERE user_id = $1;

-- name: SetDisclosurePreferences :exec
-- All three together, because the interface presents them together: a person
-- toggling one sends the state of all three, and writing them in one statement
-- means two concurrent changes cannot leave a mixture of old and new.
--
-- An upsert, since the row may not exist yet. direct_messages is left to its
-- own default on insert and untouched on update — the two settings live on one
-- row but are not one decision.
INSERT INTO user_privacy (user_id, shows_presence, shows_status, shows_last_seen)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET shows_presence = EXCLUDED.shows_presence,
    shows_status = EXCLUDED.shows_status,
    shows_last_seen = EXCLUDED.shows_last_seen,
    updated_at = NOW();
