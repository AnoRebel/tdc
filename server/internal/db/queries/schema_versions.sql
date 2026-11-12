-- name: GetSchemaVersion :one
SELECT app_version, schema_version, updated_at
FROM schema_versions
WHERE id = TRUE;

-- name: UpsertSchemaVersion :exec
INSERT INTO schema_versions (id, app_version, schema_version, updated_at)
VALUES (TRUE, $1, $2, NOW())
ON CONFLICT (id) DO UPDATE
SET app_version    = EXCLUDED.app_version,
    schema_version = EXCLUDED.schema_version,
    updated_at     = NOW();
