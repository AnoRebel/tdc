/*
A sign-in through another service, while it is still in progress.

An OAuth2 sign-in leaves this server, spends time at the provider, and comes
back to a callback with two things: a code to exchange, and the state value that
was sent out with it. Something has to remember, between those two moments, that
this particular state was issued by this server, what it was for, and what
proves the exchange belongs to the browser that started it. That is this table.

It is a table rather than a cookie because the callback must be trustworthy
without trusting the browser. A signed cookie would do for the ordinary case,
but it is replayable for as long as it is valid, and the desktop client has no
browser to hold one — it opens the system browser and waits, so the two halves
of the sign-in happen in different places entirely.

Rows are short-lived. Nothing here survives its expiry, and the sweep below is
run by the same cleanup that expires invitations.
*/
CREATE TABLE oauth_authorizations (
    id             UUID        PRIMARY KEY,

    /*
      The state parameter, stored as a hash.

      It is a bearer value for the length of the sign-in: whoever presents it at
      the callback is treated as the browser that started it. So it is stored
      the way the invitation token is, and looked up by hash, and a database
      read yields nothing anybody can use.
    */
    state_hash     BYTEA       NOT NULL UNIQUE,

    /*
      The PKCE verifier.

      Sent to the token endpoint to prove that whoever is exchanging the code is
      whoever asked for it. Held here rather than in the browser because the
      browser is not where the exchange happens — the server does it — and
      because the desktop client's callback arrives at a different place from
      where the request began.

      Stored in the clear, deliberately: it is sent in full to the provider, so
      a hash would be useless. Its protection is that the row is short-lived and
      deleted the moment it is used.
    */
    verifier       TEXT        NOT NULL,

    -- Which configured provider this belongs to. Not a foreign key: providers
    -- live in the configuration file, not in the database, so a provider that
    -- is removed leaves rows that simply never match again and expire.
    provider       TEXT        NOT NULL,

    /*
      Where to return to once this succeeds.

      Checked against what the server will allow before it is stored, not when
      it is used, so a row that exists is a row that is safe to redirect to.
    */
    redirect_to    TEXT        NOT NULL DEFAULT '',

    /*
      Who started this, when it is not a sign-in.

      Null means somebody signing in or signing up: there is no account yet, or
      none that is known. Set means somebody already signed in who is adding
      this provider to their account, and the identity that comes back may only
      be attached to that user — never used to sign in as somebody else.
    */
    user_id        UUID        REFERENCES users(id) ON DELETE CASCADE,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL,

    CONSTRAINT oauth_authorizations_expires_after_creation
        CHECK (expires_at > created_at)
);

-- The sweep that removes what has expired.
CREATE INDEX oauth_authorizations_expires_at_idx ON oauth_authorizations (expires_at);

/*
The email the provider asserted, and whether it said it had verified it.

Kept on the credential rather than copied onto the user, because it is the
provider's claim about the account at the provider, not this server's record of
the person. The two can disagree — somebody may change their email here, or
there, and neither should silently overwrite the other.

The verified flag is what the linking rule turns on. A provider that says it has
verified an address is trusted enough to offer linking to an existing account
with that address; one that has not is trusted for nothing, because anybody who
can set an unverified address at the provider could otherwise claim any account
on this server by typing its owner's email into their own profile.
*/
ALTER TABLE credentials
  ADD COLUMN external_email          TEXT,
  ADD COLUMN external_email_verified BOOLEAN NOT NULL DEFAULT FALSE;
