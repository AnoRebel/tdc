ALTER TABLE attachments DROP CONSTRAINT IF EXISTS attachments_sensitive_complete;
ALTER TABLE attachments DROP CONSTRAINT IF EXISTS attachments_sensitive_source_known;
ALTER TABLE attachments DROP COLUMN IF EXISTS sensitive_source;
ALTER TABLE attachments DROP COLUMN IF EXISTS sensitive_by;
