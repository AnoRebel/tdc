/*
Tokens that let a program act as an account, within limits the account sets.

Everything the web client does is an HTTP call somebody else can make, and this
is what lets them make it: a token carried in the same Authorization header a
session uses, on the same routes, answering identically.

A token is not a session. A session is how a person proves who they are and
carries their whole authority; a token is issued deliberately, for a program,
and carries only what it was given. They are separate tables because they have
separate lifetimes, separate revocation, and separate reasons to exist — and
because a token that appeared in somebody's list of signed-in devices would be
confusing in both directions.

Hashed, never stored: the column holds SHA-256 of the token and the token
itself is shown once. A plain hash is right here for the same reason it is
right for sessions — the value already has full entropy, so there is nothing to
brute-force, and it is looked up on every request where a memory-hard hash
would be far too slow.
*/
CREATE TABLE api_tokens (
    id           UUID        PRIMARY KEY,
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- What this token is for, in the owner's words. Shown in the list where
    -- they decide whether they still want it, so "deploy bot" earns its place
    -- and "token 3" does not.
    label        TEXT        NOT NULL,

    -- SHA-256 of the token. Unique, so two tokens cannot collide, and the
    -- index this creates is what makes the per-request lookup cheap.
    token_hash   BYTEA       NOT NULL UNIQUE,

    /*
      The permissions this token may exercise, as a JSON array of names.

      An empty array means no permission at all rather than every permission:
      a token whose scope failed to save must do nothing, not everything.

      These are a ceiling, not a grant. The token can do what its scope names
      *and* the account still holds — revoking somebody's role takes it from
      their tokens at the same moment, because the intersection is computed per
      request rather than frozen when the token was made.
    */
    scope        JSONB       NOT NULL DEFAULT '[]'::jsonb,

    -- Null means it does not expire on its own. A token for a deployment that
    -- runs for years is legitimate; so is one for an afternoon's script.
    expires_at   TIMESTAMPTZ,

    -- When it stopped working, and null while it still does. Kept rather than
    -- deleted so the audit log's reference to it still resolves to something.
    revoked_at   TIMESTAMPTZ,

    -- When it was last used, for the list where somebody decides what to
    -- revoke: a token untouched for a year is the easiest one to answer for.
    last_used_at TIMESTAMPTZ,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT api_tokens_label_not_empty CHECK (length(trim(label)) > 0),
    CONSTRAINT api_tokens_scope_is_array  CHECK (jsonb_typeof(scope) = 'array')
);

-- The owner's list, newest first, which is the only way anybody reads these.
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id, created_at DESC);
