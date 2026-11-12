DROP INDEX IF EXISTS oauth_outcomes_state_hash_idx;
ALTER TABLE oauth_outcomes DROP COLUMN IF EXISTS state_hash;
