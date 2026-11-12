-- Initial schema.
--
-- Two tables exist from the start because both are needed before any feature
-- is built: schema_versions lets the server refuse to run against a database
-- migrated by a newer release, and server_config holds the operator's
-- settings.

-- Records what the database knows about itself.
--
-- golang-migrate tracks which migrations have run; this table records the
-- application version that last ran them, so a binary can detect that the
-- database was migrated by a newer release and refuse to start rather than
-- operating on a schema it does not understand.
CREATE TABLE schema_versions (
    -- Single-row table: the CHECK constraint makes a second row impossible.
    id                BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (id),
    app_version       TEXT        NOT NULL,
    schema_version    BIGINT      NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Operator-controlled settings for this server.
--
-- Values are stored as text and parsed by the server against its known
-- settings, so adding a setting needs no migration. The source column records
-- where a value came from, so an administrator inspecting configuration can
-- see whether it was set explicitly or left at its default.
CREATE TABLE server_config (
    key         TEXT        PRIMARY KEY,
    value       TEXT        NOT NULL,
    -- True for values that must be redacted when configuration is displayed
    -- or exported.
    is_secret   BOOLEAN     NOT NULL DEFAULT FALSE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  TEXT
);
