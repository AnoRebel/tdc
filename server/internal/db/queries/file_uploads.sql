-- name: CreateFileUpload :one
INSERT INTO file_uploads (id, uploader_id, room_id, filename, size_bytes, piece_bytes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetFileUpload :one
SELECT * FROM file_uploads WHERE id = $1;

-- Records one more piece, only if it is the next one and still fits.
--
-- The count of keys is the piece number the caller sent, so a retried or
-- concurrent attempt at a piece already recorded matches nothing; whoever
-- loses deletes the object it stored.
-- name: AddFileUploadPiece :one
UPDATE file_uploads
SET piece_keys = array_append(piece_keys, @piece_key::text),
    received_bytes = received_bytes + @piece_size::bigint,
    updated_at = now()
WHERE id = @id
  AND cardinality(piece_keys) = @piece::integer
  AND received_bytes + @piece_size::bigint <= size_bytes
RETURNING *;

-- name: DeleteFileUpload :exec
DELETE FROM file_uploads WHERE id = $1;

-- name: CountFileUploadsBy :one
SELECT count(*) FROM file_uploads WHERE uploader_id = $1;

-- What uploads in progress have reserved, server-wide.
-- name: PendingUploadBytes :one
SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM file_uploads;

-- What one person's uploads in progress have reserved.
-- name: PendingUploadBytesBy :one
SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM file_uploads WHERE uploader_id = $1;

-- Uploads nobody has added to since a moment, oldest first.
-- name: ListAbandonedFileUploads :many
SELECT * FROM file_uploads WHERE updated_at < $1 ORDER BY updated_at LIMIT $2;

-- What one person's files occupy, counted once per stored file: sending the
-- same file twice costs no more space, so it costs no more quota.
-- name: AttachmentBytesBy :one
SELECT COALESCE(SUM(size_bytes), 0)::bigint
FROM (
    SELECT DISTINCT ON (storage_key) storage_key, size_bytes
    FROM attachments
    WHERE uploader_id = $1
) AS theirs;
