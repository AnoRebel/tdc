DROP TABLE IF EXISTS scheduled_messages;
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS pinned_rooms;
DROP TABLE IF EXISTS pinned_messages;
DROP TABLE IF EXISTS reactions;
ALTER TABLE attachments DROP COLUMN IF EXISTS caption;
