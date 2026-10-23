/*
Running the server: backups, restores, reloads and restarts.

None of these tables is in any backup, and a restore that replaces everything
leaves them alone. They describe the server's own operation — which archives
exist in storage, what is running on which node — rather than the community it
holds, and a restore that erased the record of itself, or the list of archives
it could be undone with, would be one nobody could account for afterwards.

No foreign keys to users for the same reason: a restore truncates users, and a
cascade would take this history with it. Who acted is kept as a label, as the
audit log does.
*/

-- Archives kept in the storage backend.
CREATE TABLE backups (
    id           UUID        PRIMARY KEY,
    -- Created by hand, by the schedule, or uploaded from elsewhere.
    origin       TEXT        NOT NULL CHECK (origin IN ('manual', 'scheduled', 'uploaded')),
    -- Creating or uploading until the archive is whole; ready once it is.
    status       TEXT        NOT NULL CHECK (status IN ('creating', 'uploading', 'ready', 'failed')),
    storage_key  TEXT        NOT NULL UNIQUE,
    size_bytes   BIGINT      NOT NULL DEFAULT 0,
    -- Pieces of an upload received so far, in order.
    pieces       INTEGER     NOT NULL DEFAULT 0,
    -- The manifest as written, or as read from an uploaded archive once its
    -- passphrase is first given. Empty until known.
    manifest     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    summary      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by   TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    error        TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX backups_created ON backups (created_at DESC);

/*
Long-running or disruptive things done to the server, with their outcome.

A backup or restore is claimed here, and the partial unique index is the lock:
one at a time across every node, because two snapshots or two restores at once
would compete for the same disk and, for restores, the same rows.
*/
CREATE TABLE operations (
    id               UUID        PRIMARY KEY,
    kind             TEXT        NOT NULL CHECK (kind IN ('backup', 'restore', 'reload', 'restart')),
    status           TEXT        NOT NULL CHECK (status IN
                         ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'refused')),
    -- The node that runs it, and the host it runs on: a restarted process has
    -- a new node identifier and the same host.
    node             TEXT        NOT NULL DEFAULT '',
    host             TEXT        NOT NULL DEFAULT '',
    backup_id        UUID,
    requested_by     TEXT        NOT NULL DEFAULT '',
    requested_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    heartbeat_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancel_requested BOOLEAN     NOT NULL DEFAULT FALSE,
    -- What was asked for, and what came of it.
    request          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    progress         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    result           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error            TEXT        NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX operations_one_job ON operations ((TRUE))
    WHERE kind IN ('backup', 'restore') AND status IN ('pending', 'running');
CREATE INDEX operations_requested ON operations (requested_at DESC);
CREATE INDEX operations_waiting ON operations (node) WHERE status = 'pending';

-- Every node, and when it last said it was alive.
CREATE TABLE server_nodes (
    id          TEXT        PRIMARY KEY,
    host        TEXT        NOT NULL,
    version     TEXT        NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    connections INTEGER     NOT NULL DEFAULT 0
);

-- Whether the server is refusing ordinary requests, and why. One row.
CREATE TABLE maintenance (
    id               BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (id),
    active           BOOLEAN     NOT NULL DEFAULT FALSE,
    reason           TEXT        NOT NULL DEFAULT '',
    operation_id     UUID,
    -- Set when only a restart can end it: the restore replaced what running
    -- processes hold in memory, such as the signing key.
    restart_required BOOLEAN     NOT NULL DEFAULT FALSE,
    since            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO maintenance (id) VALUES (TRUE);
