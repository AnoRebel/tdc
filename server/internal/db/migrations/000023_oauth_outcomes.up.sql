/*
What a sign-in through a provider turned out to be, waiting to be collected.

The callback and the client are not the same request, and on the desktop they
are not even the same program: the provider returns to a browser while the
application waits elsewhere. So the callback resolves the sign-in, leaves the
answer here, and sends the browser onward with nothing but a handle to it.

The answer is not in that handle's URL on purpose. A query string reaches
browser history, the referer header and every proxy in between, and "there is
already an account with this address" is not something to write into all three —
nor is a session token.

Collected once and then gone, like the authorization it came from.
*/
CREATE TABLE oauth_outcomes (
    id           UUID        PRIMARY KEY,

    -- The handle, hashed. It is a bearer value for the moment it exists:
    -- whoever presents it collects the sign-in, which may be a session token.
    handle_hash  BYTEA       NOT NULL UNIQUE,

    /*
      What happened, as the resolution decided it: signed-in, linked,
      link-offered, new-account, or one of the ways it did not happen.

      Text rather than an enum because the set grows with the feature, and a
      value this server does not recognise is one it refuses rather than one
      that breaks a migration.
    */
    status       TEXT        NOT NULL,

    /*
      The rest of the answer, as JSON.

      A column per field would mean a migration every time an outcome carries
      something new, and what is here differs by status: a session token for one,
      a provider's suggestions for another, an account to offer a link to for a
      third. It is written and read whole by one package.
    */
    detail       JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL,

    CONSTRAINT oauth_outcomes_expires_after_creation CHECK (expires_at > created_at),
    CONSTRAINT oauth_outcomes_detail_is_object CHECK (jsonb_typeof(detail) = 'object')
);

CREATE INDEX oauth_outcomes_expires_at_idx ON oauth_outcomes (expires_at);
