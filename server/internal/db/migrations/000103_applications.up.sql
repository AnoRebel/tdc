/*
Applications: programs installed on this server as themselves.

Distinct from an API token, which a person issues to let a program act as them.
An application is registered, explicitly approved by somebody allowed to, given
exactly the permissions it was approved for and no more, restricted to the
channels it was approved for, and removed as a whole — its credentials, its
standing, and if the operator chooses, what it posted.

It acts through an account of its own, made when it is approved and never
signed into, so the machinery every request already goes through — membership,
suspension, moderation, rate limits — applies to it unchanged. What makes that
account an application's is this table pointing at it; what it may do comes
from here, never from the account's roles and never from whoever approved it.
*/
CREATE TABLE applications (
    id                    UUID        PRIMARY KEY,
    name                  TEXT        NOT NULL,
    purpose               TEXT        NOT NULL,
    -- What it asked for, by permission name.
    requested_permissions TEXT[]      NOT NULL DEFAULT '{}',
    /*
      Whether it sends anything to a service outside this server, and if so
      what and where. Shown to whoever approves it, before they approve, and
      to everybody afterwards.
    */
    sends_data_externally BOOLEAN     NOT NULL DEFAULT false,
    external_disclosure   TEXT        NOT NULL DEFAULT '',
    /*
      registered — recorded, and able to do nothing at all
      approved   — holding exactly granted_permissions, in its channels
      removed    — gone, its credentials revoked
    */
    state                 TEXT        NOT NULL DEFAULT 'registered',
    -- What was approved: a copy of the request at the moment of approval, so
    -- a request changed afterwards grants nothing until approved again.
    granted_permissions   TEXT[]      NOT NULL DEFAULT '{}',
    registered_by         UUID        REFERENCES users(id) ON DELETE SET NULL,
    registered_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_by           UUID        REFERENCES users(id) ON DELETE SET NULL,
    approved_at           TIMESTAMPTZ,
    removed_by            UUID        REFERENCES users(id) ON DELETE SET NULL,
    removed_at            TIMESTAMPTZ,
    -- The account it acts through, made at approval.
    account_id            UUID        UNIQUE REFERENCES users(id) ON DELETE SET NULL,
    -- Requests refused by its own rate limit, and when last.
    rate_limited_count    BIGINT      NOT NULL DEFAULT 0,
    last_rate_limited_at  TIMESTAMPTZ,

    CONSTRAINT applications_state CHECK (state IN ('registered', 'approved', 'removed')),
    CONSTRAINT applications_name CHECK (length(trim(name)) > 0),
    CONSTRAINT applications_purpose CHECK (length(trim(purpose)) > 0),
    CONSTRAINT applications_disclosure CHECK (
        NOT sends_data_externally OR length(trim(external_disclosure)) > 0)
);

-- The channels an application may act in. Nothing outside this list, ever.
CREATE TABLE application_channels (
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    room_id        UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    PRIMARY KEY (application_id, room_id)
);

CREATE INDEX application_channels_room_idx ON application_channels (room_id);

/*
An API token may belong to an application rather than to a person.

The same table and the same lookup, so revoking one is the same immediate act,
but a token with this set is resolved against the application's grant rather
than its account's roles. Removed with the application.
*/
ALTER TABLE api_tokens
    ADD COLUMN application_id UUID REFERENCES applications(id) ON DELETE CASCADE;

CREATE INDEX api_tokens_application_idx ON api_tokens (application_id) WHERE application_id IS NOT NULL;
