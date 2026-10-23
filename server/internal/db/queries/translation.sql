-- name: GetTranslationPreferences :one
SELECT * FROM translation_preferences WHERE user_id = $1;

-- name: SetTranslationPreferences :exec
INSERT INTO translation_preferences (user_id, provider, target_language, auto_translate)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET provider = EXCLUDED.provider,
    target_language = EXCLUDED.target_language,
    auto_translate = EXCLUDED.auto_translate,
    updated_at = NOW();

-- name: ListTranslationRoomRules :many
SELECT * FROM translation_room_rules WHERE user_id = $1 ORDER BY room_id;

-- name: SetTranslationRoomRule :exec
INSERT INTO translation_room_rules (user_id, room_id, auto_translate)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, room_id) DO UPDATE SET auto_translate = EXCLUDED.auto_translate;

-- name: DeleteTranslationRoomRule :exec
DELETE FROM translation_room_rules WHERE user_id = $1 AND room_id = $2;

-- name: ListTranslationConnections :many
SELECT * FROM translation_connections WHERE user_id = $1 ORDER BY provider;

-- name: GetTranslationConnection :one
SELECT * FROM translation_connections WHERE user_id = $1 AND provider = $2;

-- name: SetTranslationConnection :exec
INSERT INTO translation_connections (user_id, provider, sealed_key, endpoint, region)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, provider) DO UPDATE
SET sealed_key = EXCLUDED.sealed_key,
    endpoint = EXCLUDED.endpoint,
    region = EXCLUDED.region,
    updated_at = NOW();

-- name: DeleteTranslationConnection :exec
DELETE FROM translation_connections WHERE user_id = $1 AND provider = $2;

-- name: GetMessageTranslation :one
SELECT * FROM message_translations
WHERE message_id = $1 AND target_language = $2 AND provider = $3 AND body_hash = $4;

-- name: StoreMessageTranslation :exec
-- Older translations of the same message, of a body it no longer has, go:
-- an edited message's earlier words should not stay behind in the cache.
WITH stale AS (
    DELETE FROM message_translations
    WHERE message_id = @message_id AND body_hash <> @body_hash
)
INSERT INTO message_translations (
    message_id, source_event_id, target_language, provider,
    body_hash, translated_body, source_language
)
VALUES (
    @message_id, @source_event_id, @target_language, @provider,
    @body_hash, @translated_body, @source_language
)
ON CONFLICT (message_id, target_language, provider, body_hash) DO UPDATE
SET translated_body = EXCLUDED.translated_body,
    source_language = EXCLUDED.source_language,
    created_at = NOW();
