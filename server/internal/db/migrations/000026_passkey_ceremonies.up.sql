/*
A passkey ceremony, while it is still happening.

Registering or signing in with a passkey takes two requests. The first asks the
server for a challenge; the second brings back what the authenticator did with
it. Between them something has to remember which challenge was issued, to whom,
and what it was for — and it has to be the server that remembers, because a
challenge the caller hands back to itself proves nothing.

A table rather than a cookie, for the same reasons the sign-in authorizations
are one: the desktop client has no browser cookie jar of its own, and a signed
cookie is replayable for as long as it is valid where a row can be deleted the
moment it is used.

Short-lived and swept, like everything else of this shape here.
*/
CREATE TABLE webauthn_ceremonies (
    id           UUID        PRIMARY KEY,

    /*
      The challenge, stored as a hash.

      It is the value that must come back, so whoever holds it is treated as the
      browser that asked for it. Hashed for the same reason the sign-in state
      is: a database read should not yield a working ceremony.
    */
    challenge_hash BYTEA     NOT NULL UNIQUE,

    /*
      The library's own session data, as JSON.

      Written whole and read whole; the database never looks inside it. It
      carries the challenge itself, which the library needs in the form it
      issued — the hash above is the lookup key, not a replacement for it.
    */
    session      JSONB       NOT NULL,

    -- 'register' or 'login'. A registration challenge must not complete a
    -- sign-in, nor the reverse, so what it was for is recorded rather than
    -- inferred from whether a user is attached.
    purpose      TEXT        NOT NULL,

    /*
      Whose ceremony this is.

      Set when somebody signed in is enrolling a passkey, or when a sign-in
      named an account first. Null for a passkey sign-in with nothing typed,
      where the authenticator chooses and the account is only known afterwards.
    */
    user_id      UUID        REFERENCES users(id) ON DELETE CASCADE,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL,

    CONSTRAINT webauthn_ceremonies_purpose_valid
        CHECK (purpose IN ('register', 'login')),
    CONSTRAINT webauthn_ceremonies_expires_after_creation
        CHECK (expires_at > created_at)
);

CREATE INDEX webauthn_ceremonies_expires_at_idx ON webauthn_ceremonies (expires_at);
