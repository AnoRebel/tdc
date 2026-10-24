-- File transfers: uploads sent in pieces, files from other servers, and
-- direct transfers between two people's devices.

-- An upload in progress, sent in pieces.
--
-- Each piece is its own object in the store, named in piece_keys in order, so
-- any node behind a load balancer can take any piece and nothing has to be
-- held in one process's memory or on one machine's disk. The declared size is
-- what the limits are checked against when it begins, and what it reserves
-- against them until it finishes or is abandoned.
CREATE TABLE file_uploads (
    id             UUID        PRIMARY KEY,
    uploader_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id        UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    filename       TEXT        NOT NULL,
    size_bytes     BIGINT      NOT NULL,
    piece_bytes    INTEGER     NOT NULL,
    -- Every piece received so far, in order. A piece's key carries a random
    -- part, so two attempts at the same piece never overwrite each other and
    -- the one that loses can delete its own object without touching the
    -- winner's.
    piece_keys     TEXT[]      NOT NULL DEFAULT '{}',
    received_bytes BIGINT      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT file_uploads_size CHECK (size_bytes >= 0),
    CONSTRAINT file_uploads_piece CHECK (piece_bytes > 0),
    CONSTRAINT file_uploads_received CHECK (received_bytes >= 0 AND received_bytes <= size_bytes)
);

CREATE INDEX file_uploads_uploader_idx ON file_uploads (uploader_id);
-- Finding uploads nobody came back to, for the sweep.
CREATE INDEX file_uploads_updated_idx ON file_uploads (updated_at);

-- A file a member of another server sent.
--
-- origin_server is the server whose member sent it, and is null for a file
-- uploaded here. Its bytes are copied on first view, after which fetched_at is
-- set; until then the row is a description and the storage key is the digest
-- the author's server signed.
ALTER TABLE attachments
    ADD COLUMN origin_server TEXT,
    ADD COLUMN fetched_at    TIMESTAMPTZ;

-- A peer asks for a file by its digest within a room.
CREATE INDEX attachments_room_key_idx ON attachments (room_id, storage_key);

-- One person offering a file straight to another's device.
--
-- A row rather than memory because the two people may be connected to
-- different nodes, and whichever node relays their negotiation must be able to
-- check it belongs to an offer between them. The bytes never come near it.
CREATE TABLE direct_transfers (
    id           UUID        PRIMARY KEY,
    room_id      UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- What the sender says it is. Shown to the recipient as a claim.
    filename     TEXT        NOT NULL,
    size_bytes   BIGINT      NOT NULL,
    content_type TEXT        NOT NULL DEFAULT '',
    state        TEXT        NOT NULL DEFAULT 'offered',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- An offer nobody answers lapses; an accepted one has this long to finish.
    expires_at   TIMESTAMPTZ NOT NULL,
    ended_at     TIMESTAMPTZ,

    CONSTRAINT direct_transfers_size CHECK (size_bytes >= 0),
    CONSTRAINT direct_transfers_people CHECK (sender_id <> recipient_id),
    CONSTRAINT direct_transfers_state CHECK (state IN
        ('offered', 'accepted', 'declined', 'cancelled', 'finished', 'failed', 'expired'))
);

CREATE INDEX direct_transfers_recipient_idx ON direct_transfers (recipient_id, state);
CREATE INDEX direct_transfers_sender_idx ON direct_transfers (sender_id, state);
CREATE INDEX direct_transfers_expires_idx ON direct_transfers (expires_at);
