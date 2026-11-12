-- name: CreateAttachment :one
INSERT INTO attachments (
    id, storage_key, uploader_id, uploader_address, room_id,
    filename, content_type, size_bytes,
    preview_key, preview_type, preview_width, preview_height, width, height,
    caption, animated
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
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
WHERE attachments.event_id IS NULL AND attachments.created_at < $1
  -- An event's cover is held by the event rather than by a message.
  AND NOT EXISTS (SELECT 1 FROM calendar_events covers WHERE covers.cover_attachment_id = attachments.id)
ORDER BY attachments.created_at
LIMIT $2;

-- Counts what still points at a stored preview, so its bytes are only removed
-- once nothing references them.
-- name: CountAttachmentsWithPreviewKey :one
SELECT count(*) FROM attachments WHERE preview_key = $1;

-- name: SetAttachmentCaption :execrows
-- Scoped to the uploader and to attachments not yet sent: a caption is written
-- while composing, and a message already sent is not edited this way.
UPDATE attachments
SET caption = $3
WHERE id = $1 AND uploader_id = $2 AND event_id IS NULL;

-- name: TotalAttachmentBytes :one
-- Storage in use, for the operator's capacity limit.
--
-- Counted by distinct stored object rather than by attachment: the same file
-- sent twice occupies one place, so charging it twice would refuse uploads
-- against space that is not being used.
-- A file from another server counts once its bytes have been copied here, and
-- not before: until then it occupies nothing.
SELECT COALESCE(SUM(size_bytes), 0)::bigint
FROM (
    SELECT DISTINCT ON (storage_key) storage_key, size_bytes FROM attachments
    WHERE origin_server IS NULL OR fetched_at IS NOT NULL
) AS unique_objects;

-- name: MarkAttachmentSensitive :execrows
-- Marks media so viewers see it obscured until they choose otherwise.
--
-- A moderator's mark overwrites an uploader's; an uploader's does not
-- overwrite a moderator's. Expressed here rather than in Go so the rule holds
-- for every caller, and so two people marking at once cannot interleave into
-- the weaker outcome.
UPDATE attachments
SET sensitive_by = $2, sensitive_source = $3
WHERE id = $1
  AND (sensitive_source IS DISTINCT FROM 'moderator' OR $3 = 'moderator');

-- name: UnmarkAttachmentSensitive :execrows
-- Removes a mark.
--
-- Scoped by who may: an uploader may undo their own mark and an automatic one
-- on their own file, but not a moderator's — otherwise marking would be
-- advisory, and the people most likely to post something unwelcome would be
-- exactly the people able to unmark it.
UPDATE attachments
SET sensitive_by = NULL, sensitive_source = NULL
WHERE id = $1
  AND (
    -- A moderator may lift any mark.
    @as_moderator::boolean
    -- An uploader may lift their own, and an automatic one on their own file.
    OR (uploader_id = @actor_id::uuid AND sensitive_source <> 'moderator')
  );

-- Records a file a member of another server sent, from the description its
-- message carries. The bytes are not here yet; fetched_at stays empty until
-- somebody opens it and they are copied.
-- name: CreateRemoteAttachment :one
INSERT INTO attachments (
    id, storage_key, uploader_address, room_id, event_id,
    filename, content_type, size_bytes, width, height, caption, origin_server
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- The file a sent message in a room carries, by its digest: what a peer asks
-- for. A copy held here is preferred over a description of one not yet copied.
-- name: RoomFileByKey :one
SELECT * FROM attachments
WHERE room_id = $1 AND storage_key = $2 AND event_id IS NOT NULL
ORDER BY (origin_server IS NULL OR fetched_at IS NOT NULL) DESC, created_at
LIMIT 1;

-- Every description of a file from elsewhere becomes a copy once its bytes are
-- here, whichever room it was opened in: the bytes are the same bytes.
-- name: MarkRemoteAttachmentsFetched :exec
UPDATE attachments
SET fetched_at = now(),
    content_type = @content_type,
    animated = @animated,
    width = COALESCE(sqlc.narg(width), width),
    height = COALESCE(sqlc.narg(height), height),
    preview_key = sqlc.narg(preview_key),
    preview_type = sqlc.narg(preview_type),
    preview_width = sqlc.narg(preview_width),
    preview_height = sqlc.narg(preview_height)
WHERE storage_key = @storage_key AND origin_server IS NOT NULL;

-- name: ListAttachmentsForEvents :many
-- The files carried by a set of messages, so expiring the messages can remove
-- them too.
SELECT * FROM attachments WHERE event_id = ANY(@event_ids::text[]);
