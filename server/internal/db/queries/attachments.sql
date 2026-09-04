-- name: CreateAttachment :one
INSERT INTO attachments (
    id, storage_key, uploader_id, uploader_address, room_id,
    filename, content_type, size_bytes,
    preview_key, preview_type, preview_width, preview_height, width, height
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = $1;

-- name: ListAttachmentsForEvent :many
SELECT * FROM attachments
WHERE event_id = $1
ORDER BY created_at;

-- Binds an attachment to the message that carries it.
--
-- Scoped to the uploader and to attachments not already attached, so one
-- person cannot claim another's upload, and an attachment cannot be moved
-- between messages after the fact.
-- name: AttachToEvent :execrows
UPDATE attachments
SET event_id = $1
WHERE id = ANY(@attachment_ids::uuid[])
  AND uploader_id = $2
  AND room_id = $3
  AND event_id IS NULL;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = $1;

-- Counts what still points at a stored object, so the bytes are only removed
-- once nothing references them.
-- name: CountAttachmentsWithStorageKey :one
SELECT count(*) FROM attachments WHERE storage_key = $1;

-- Attachments that never became part of a message, oldest first.
-- name: ListOrphanedAttachments :many
SELECT * FROM attachments
WHERE event_id IS NULL AND created_at < $1
ORDER BY created_at
LIMIT $2;

-- Counts what still points at a stored preview, so its bytes are only removed
-- once nothing references them.
-- name: CountAttachmentsWithPreviewKey :one
SELECT count(*) FROM attachments WHERE preview_key = $1;
