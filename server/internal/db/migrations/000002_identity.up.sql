-- Accounts, credentials, sessions and the permission model.

-- A person on this server.
--
-- Identity is deliberately split from credentials: a user may authenticate by
-- password, passkey or an external provider, and may hold several at once.
CREATE TABLE users (
    id              UUID        PRIMARY KEY,
    -- The local part of the user's address. Combined with the server's own
    -- name it forms user@server, which stays unambiguous once servers
    -- federate.
    username        TEXT        NOT NULL,
    -- Lower-cased username, unique. Stored rather than computed so the unique
    -- index is a plain b-tree and lookups need no function call.
    username_folded TEXT        NOT NULL UNIQUE,
    display_name    TEXT        NOT NULL DEFAULT '',
    email           TEXT,
    email_folded    TEXT UNIQUE,
    email_verified  BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Suspension is a moderator action, so it carries who and why.
    suspended_at    TIMESTAMPTZ,
    suspended_until TIMESTAMPTZ,
    suspended_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
    suspension_note TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT users_username_not_empty CHECK (length(trim(username)) > 0)
);

CREATE INDEX users_created_at_idx ON users (created_at DESC);

-- A way for a user to prove who they are.
--
-- One row per credential, so a user can hold a password and several passkeys
-- at once, and any one can be revoked without disturbing the others.
CREATE TABLE credentials (
    id            UUID        PRIMARY KEY,
    user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 'password', 'passkey' or 'oauth2'.
    kind          TEXT        NOT NULL,
    -- A name the user gave this credential, to tell their devices apart.
    label         TEXT        NOT NULL DEFAULT '',
    -- Kind-specific material: an argon2id hash, a WebAuthn public key, or an
    -- external subject identifier. Never a recoverable secret.
    secret        BYTEA,
    -- For passkeys, the credential ID the authenticator returns; for OAuth2,
    -- the provider's subject. Unique so one authenticator cannot be
    -- registered twice.
    external_id   TEXT,
    -- For OAuth2, which provider this came from.
    provider      TEXT,
    -- WebAuthn signature counter, for detecting cloned authenticators.
    sign_count    BIGINT      NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ,

    CONSTRAINT credentials_kind_valid CHECK (kind IN ('password', 'passkey', 'oauth2'))
);

CREATE INDEX credentials_user_id_idx ON credentials (user_id);
CREATE UNIQUE INDEX credentials_external_id_idx
    ON credentials (kind, provider, external_id)
    WHERE external_id IS NOT NULL;

-- A user may hold at most one password.
CREATE UNIQUE INDEX credentials_one_password_per_user_idx
    ON credentials (user_id)
    WHERE kind = 'password';

-- An authenticated session on one device.
CREATE TABLE sessions (
    id              UUID        PRIMARY KEY,
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- SHA-256 of the session token. The token itself is never stored, so a
    -- database disclosure does not hand over live sessions.
    token_hash      BYTEA       NOT NULL UNIQUE,
    -- Shown to the user so they can recognise a session well enough to decide
    -- whether to end it.
    user_agent      TEXT        NOT NULL DEFAULT '',
    ip_address      INET,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at) WHERE revoked_at IS NULL;

-- A named bundle of permissions.
CREATE TABLE roles (
    id          UUID        PRIMARY KEY,
    name        TEXT        NOT NULL UNIQUE,
    description TEXT        NOT NULL DEFAULT '',
    -- Built-in roles cannot be deleted, so a server always retains a coherent
    -- permission model.
    built_in    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Permissions a role grants or explicitly denies.
--
-- A denial is stored rather than implied by absence, because an explicit
-- denial at a narrow scope must override a grant at a broader one.
CREATE TABLE role_permissions (
    role_id     UUID    NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission  TEXT    NOT NULL,
    -- NULL means server scope; otherwise the channel this applies to.
    scope_id    UUID,
    granted     BOOLEAN NOT NULL DEFAULT TRUE,

    PRIMARY KEY (role_id, permission, scope_id)
);

-- Roles held by a user.
CREATE TABLE user_roles (
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     UUID        NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by  UUID        REFERENCES users(id) ON DELETE SET NULL,

    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX user_roles_role_id_idx ON user_roles (role_id);

-- Security- and administration-relevant actions.
--
-- Append-only: no update or delete path exists in the application. Entries
-- reference content by identifier rather than copying it, so the log never
-- becomes a second store of message text.
CREATE TABLE audit_entries (
    id           BIGSERIAL   PRIMARY KEY,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- NULL when the actor is the system or an unauthenticated caller.
    actor_id     UUID        REFERENCES users(id) ON DELETE SET NULL,
    -- Retained even when the account is gone, so the record stays meaningful.
    actor_label  TEXT        NOT NULL DEFAULT '',
    action       TEXT        NOT NULL,
    target_kind  TEXT        NOT NULL DEFAULT '',
    target_id    TEXT        NOT NULL DEFAULT '',
    -- Where the request came from.
    source_ip    INET,
    user_agent   TEXT        NOT NULL DEFAULT '',
    -- Structured detail: previous and new values for a setting change, the
    -- reason for a moderator action. Never credentials or message content.
    detail       JSONB       NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX audit_entries_occurred_at_idx ON audit_entries (occurred_at DESC);
CREATE INDEX audit_entries_actor_idx ON audit_entries (actor_id, occurred_at DESC);
CREATE INDEX audit_entries_action_idx ON audit_entries (action, occurred_at DESC);
