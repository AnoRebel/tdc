DROP TABLE IF EXISTS remote_members;
ALTER TABLE room_peers DROP CONSTRAINT IF EXISTS room_peers_failures_not_negative;
ALTER TABLE room_peers DROP COLUMN IF EXISTS abandoned;
ALTER TABLE room_peers DROP COLUMN IF EXISTS next_attempt_at;
ALTER TABLE room_peers DROP COLUMN IF EXISTS failures;
