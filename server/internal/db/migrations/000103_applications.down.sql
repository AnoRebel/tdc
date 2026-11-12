DROP INDEX IF EXISTS api_tokens_application_idx;
DELETE FROM api_tokens WHERE application_id IS NOT NULL;
ALTER TABLE api_tokens DROP COLUMN IF EXISTS application_id;
DROP TABLE IF EXISTS application_channels;
DROP TABLE IF EXISTS applications;
