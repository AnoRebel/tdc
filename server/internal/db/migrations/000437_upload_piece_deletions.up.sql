/*
Pieces of an upload whose row went away without its pieces being removed.

An upload's row is the only record of where its pieces are, and it is deleted
by cascade when its account or room is: nothing in Go sees that happen, so
the pieces would stay in storage for good. Every deletion of an upload queues
its pieces here, and the sweep that removes abandoned uploads empties it.
Deleting an object already gone is harmless, so an upload removed the
ordinary way, which deletes its own pieces, costs a second look and nothing
more.
*/
CREATE TABLE storage_deletions (
    key       TEXT        PRIMARY KEY,
    queued_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE FUNCTION queue_upload_pieces() RETURNS trigger AS $$
BEGIN
    INSERT INTO storage_deletions (key) SELECT unnest(OLD.piece_keys) ON CONFLICT DO NOTHING;
    RETURN OLD;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER file_uploads_queue_pieces AFTER DELETE ON file_uploads
    FOR EACH ROW EXECUTE FUNCTION queue_upload_pieces();
