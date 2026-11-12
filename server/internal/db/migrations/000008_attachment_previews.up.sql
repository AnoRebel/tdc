-- Generated previews for attachments.

-- A preview is stored beside the file it previews, under its own key, so that
-- serving one never touches the original. Null where no preview exists, which
-- is the common case: most attachments are not images.
ALTER TABLE attachments ADD COLUMN preview_key TEXT;
ALTER TABLE attachments ADD COLUMN preview_type TEXT;
ALTER TABLE attachments ADD COLUMN preview_width INTEGER;
ALTER TABLE attachments ADD COLUMN preview_height INTEGER;

-- The original's dimensions, where they are known.
--
-- Recorded so a client can reserve the right space before the image arrives,
-- which is what stops a conversation jumping around as attachments load.
ALTER TABLE attachments ADD COLUMN width INTEGER;
ALTER TABLE attachments ADD COLUMN height INTEGER;

-- Previews share the reclamation path with the files themselves.
CREATE INDEX attachments_preview_key_idx ON attachments (preview_key)
    WHERE preview_key IS NOT NULL;
