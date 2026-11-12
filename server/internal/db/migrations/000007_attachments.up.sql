-- Files attached to messages.

-- An attachment is uploaded before the message that carries it exists.
--
-- The alternative is to accept the file and the message in one request, which
-- means holding the whole upload in memory or on disk before anything can be
-- validated, and makes a failed send lose the file. Uploading first gives the
-- client an identifier to reference, lets the server refuse an oversized or
-- disallowed file immediately, and makes a retry of the message cost nothing.
--
-- The consequence is that an attachment can exist without a message, which is
-- what the orphan reclamation in a later phase is for.
CREATE TABLE attachments (
    id           UUID        PRIMARY KEY,
    -- Where the bytes live in the object store. Content-derived, so the same
    -- file uploaded twice occupies one place, and knowing an identifier does
    -- not let anyone guess another.
    storage_key  TEXT        NOT NULL,
    -- Who uploaded it. Retained after the account is deleted so the audit
    -- trail survives, in the same way events keep a sender address.
    uploader_id  UUID        REFERENCES users(id) ON DELETE SET NULL,
    uploader_address TEXT    NOT NULL,
    -- The room it was uploaded for. Retrieval authorisation is the room's, so
    -- an attachment cannot outlive the permissions that govern it.
    room_id      UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    -- The event that carries it, once one exists. Null until the message is
    -- sent, and the reason an unreferenced attachment can be reclaimed.
    event_id     TEXT        REFERENCES events(id) ON DELETE CASCADE,
    -- The name the uploader gave, kept only to name the download. Never used
    -- to decide where the file goes or what it is.
    filename     TEXT        NOT NULL,
    -- Detected from the content, never from what the client declared.
    content_type TEXT        NOT NULL,
    size_bytes   BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT attachments_size_positive CHECK (size_bytes >= 0)
);

-- Listing a message's attachments is the common read.
CREATE INDEX attachments_event_idx ON attachments (event_id)
    WHERE event_id IS NOT NULL;

-- Finding attachments that never became part of a message, for reclamation.
CREATE INDEX attachments_orphan_idx ON attachments (created_at)
    WHERE event_id IS NULL;

-- Several attachments may share one stored object when their contents match,
-- so the store is only safe to delete from once nothing references the key.
CREATE INDEX attachments_storage_key_idx ON attachments (storage_key);
