DROP TABLE IF EXISTS direct_transfers;
DROP INDEX IF EXISTS attachments_room_key_idx;
ALTER TABLE attachments
    DROP COLUMN IF EXISTS fetched_at,
    DROP COLUMN IF EXISTS origin_server;
DROP TABLE IF EXISTS file_uploads;
