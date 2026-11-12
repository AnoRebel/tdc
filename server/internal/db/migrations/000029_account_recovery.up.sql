/*
A way back into an account whose password is gone.

One row per request, holding a hash of the token that was mailed out. The token
itself exists only in that message and in whatever the person does with the link
— a database read yields nothing that gets anybody in, which is the same reason
an invitation and a session are stored this way.

Deliberately not on the credentials table. A recovery token is not a way to
authenticate: it is permission to replace the thing that does, once, soon, and
then never again.
*/
CREATE TABLE recovery_tokens (
    id          UUID        PRIMARY KEY,

    token_hash  BYTEA       NOT NULL UNIQUE,

    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    /*
      Where it was sent.

      Recorded because the address on the account may change afterwards, and a
      token that arrived at the old one must not be usable to take over what the
      new one now protects. Checked when it is redeemed.
    */
    sent_to     TEXT        NOT NULL,

    /*
      Who asked.

      Not to rate-limit — that is done before a row exists — but so an
      administrator looking at an account takeover afterwards can see where the
      request came from. It is one of the few records of somebody acting without
      being signed in.
    */
    requested_from INET,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,

    /*
      When it was spent.

      Kept rather than deleted, so "this link has already been used" is
      answerable and so the audit log has something to point at. The query that
      spends one requires this to be null, which is what makes it single-use
      under two simultaneous attempts rather than by convention.
    */
    used_at     TIMESTAMPTZ,

    CONSTRAINT recovery_tokens_expires_after_creation CHECK (expires_at > created_at)
);

-- Finding the outstanding tokens for an account, which is what issuing a new one
-- invalidates, and the sweep.
CREATE INDEX recovery_tokens_user_id_idx ON recovery_tokens (user_id) WHERE used_at IS NULL;
CREATE INDEX recovery_tokens_expires_at_idx ON recovery_tokens (expires_at);
