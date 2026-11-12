DROP INDEX IF EXISTS attachments_preview_key_idx;
ALTER TABLE attachments
    DROP COLUMN IF EXISTS preview_key,
    DROP COLUMN IF EXISTS preview_type,
    DROP COLUMN IF EXISTS preview_width,
    DROP COLUMN IF EXISTS preview_height,
    DROP COLUMN IF EXISTS width,
    DROP COLUMN IF EXISTS height;
