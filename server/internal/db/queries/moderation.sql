-- name: BanFromChannel :exec
-- Re-banning somebody already banned updates the terms rather than failing:
-- a moderator extending a ban means the new terms, not an error.
INSERT INTO channel_bans (room_id, user_id, banned_by, banned_by_address, reason, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (room_id, user_id) DO UPDATE
SET banned_by = EXCLUDED.banned_by,
    banned_by_address = EXCLUDED.banned_by_address,
    reason = EXCLUDED.reason,
    expires_at = EXCLUDED.expires_at,
    created_at = now();

-- name: LiftChannelBan :execrows
DELETE FROM channel_bans WHERE room_id = $1 AND user_id = $2;

-- name: IsBannedFromChannel :one
-- Asked before joining and before posting.
--
-- The expiry is evaluated here rather than by a sweep, so a temporary ban
-- lifts at its moment rather than whenever a background job next runs.
SELECT EXISTS (
    SELECT 1 FROM channel_bans
    WHERE room_id = $1 AND user_id = $2
      AND (expires_at IS NULL OR expires_at > now())
);

-- name: ListChannelBans :many
SELECT b.room_id, b.user_id, b.banned_by_address, b.reason, b.expires_at, b.created_at,
       u.username, u.display_name
FROM channel_bans b
JOIN users u ON u.id = b.user_id
WHERE b.room_id = $1 AND (b.expires_at IS NULL OR b.expires_at > now())
ORDER BY b.created_at DESC;

-- name: DeleteLapsedBans :execrows
-- Housekeeping only. The expiry is already honoured on read, so this removes
-- rows rather than changing any answer.
DELETE FROM channel_bans WHERE expires_at IS NOT NULL AND expires_at <= now();

-- name: RestrictPosting :one
INSERT INTO posting_restrictions (
    id, user_id, room_id, restricted_by, restricted_by_address, reason, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: LiftPostingRestriction :execrows
-- Within the scope the request names — one channel, or the whole server when
-- the room is null — so lifting by identifier cannot reach a restriction made
-- somewhere else.
UPDATE posting_restrictions
SET lifted_at = now(), lifted_by = $2
WHERE id = $1 AND room_id IS NOT DISTINCT FROM $3 AND lifted_at IS NULL;

-- name: ListPostingRestrictionsInRoom :many
-- Who is currently silenced in one channel.
SELECT r.*, u.username
FROM posting_restrictions r
JOIN users u ON u.id = r.user_id
WHERE r.room_id = $1
  AND r.lifted_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > now())
ORDER BY r.created_at DESC;

-- name: IsPostingRestricted :one
-- A restriction on this channel, or one on the whole server.
--
-- Both are checked in one query: asking twice would mean two round trips on
-- the path every message takes.
SELECT EXISTS (
    SELECT 1 FROM posting_restrictions
    WHERE user_id = $1
      AND (room_id IS NULL OR room_id = $2)
      AND lifted_at IS NULL
      AND (expires_at IS NULL OR expires_at > now())
);

-- name: ListServerPostingRestrictions :many
-- Who is currently silenced everywhere on the server.
SELECT r.*, u.username
FROM posting_restrictions r
JOIN users u ON u.id = r.user_id
WHERE r.room_id IS NULL
  AND r.lifted_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > now())
ORDER BY r.created_at DESC;

-- name: BlockUser :exec
INSERT INTO user_blocks (blocker_id, blocked_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UnblockUser :exec
DELETE FROM user_blocks WHERE blocker_id = $1 AND blocked_id = $2;

-- name: HasBlocked :one
SELECT EXISTS (
    SELECT 1 FROM user_blocks WHERE blocker_id = $1 AND blocked_id = $2
);

-- name: ListBlocked :many
-- Who somebody has blocked, which only they can see.
SELECT u.id, u.username, u.display_name, b.created_at
FROM user_blocks b
JOIN users u ON u.id = b.blocked_id
WHERE b.blocker_id = $1
ORDER BY b.created_at DESC;

-- name: SetDirectMessagePolicy :exec
INSERT INTO user_privacy (user_id, direct_messages, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE
SET direct_messages = EXCLUDED.direct_messages, updated_at = now();

-- name: GetDirectMessagePolicy :one
SELECT direct_messages FROM user_privacy WHERE user_id = $1;

-- name: SharesAChannel :one
-- Whether two people are in any channel together, for the 'shared' policy.
--
-- Direct conversations are excluded: two people already in a direct
-- conversation share a room by definition, which would make 'shared' mean
-- 'anyone who has ever written to me'.
SELECT EXISTS (
    SELECT 1
    FROM room_members a
    JOIN room_members b ON b.room_id = a.room_id
    JOIN rooms r ON r.id = a.room_id
    WHERE a.user_id = $1 AND b.user_id = $2 AND r.kind <> 'direct'
);

-- name: SetCallPolicy :exec
INSERT INTO user_privacy (user_id, calls, calls_from_other_servers, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id) DO UPDATE
SET calls = EXCLUDED.calls, calls_from_other_servers = EXCLUDED.calls_from_other_servers, updated_at = now();

-- name: GetCallPolicy :one
SELECT calls, calls_from_other_servers FROM user_privacy WHERE user_id = $1;
