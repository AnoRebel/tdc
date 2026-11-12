ALTER TABLE users DROP COLUMN IF EXISTS sends_typing_indication;
DROP TABLE IF EXISTS typing_indicators;
DROP TABLE IF EXISTS server_statuses;
DROP TABLE IF EXISTS user_presence;
DROP INDEX IF EXISTS rooms_participant_key_idx;
ALTER TABLE rooms DROP COLUMN IF EXISTS participant_key;
DROP INDEX IF EXISTS events_thread_idx;
ALTER TABLE events DROP COLUMN IF EXISTS thread_root_id;
