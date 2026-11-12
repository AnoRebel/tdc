DROP INDEX IF EXISTS webhook_deliveries_application_idx;
ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS application_id;
ALTER TABLE applications
    DROP COLUMN IF EXISTS invocation_secret,
    DROP COLUMN IF EXISTS invocation_url;
