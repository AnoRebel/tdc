/*
Proving an address belongs to the person who typed it.

Until this exists, an email on an account is a claim: somebody wrote it in a
form, and nothing checked. That is why recovery refuses to mail an unverified
address — a way back into an account, sent to an address nobody proved, is a way
into somebody else's account.

Same shape as the recovery tokens beside it, and for the same reasons: the token
is hashed, single-use, short-lived, and the row records what it was for so a
confirmation cannot be turned into something else.
*/
CREATE TABLE email_confirmations (
    id          UUID        PRIMARY KEY,

    token_hash  BYTEA       NOT NULL UNIQUE,

    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    /*
      The address being proved.

      Held here rather than read from the account, because the two are
      deliberately different until this is confirmed: somebody changing their
      address keeps the old one working until the new one is proved, so a
      mistyped address does not cost them the one they had.
    */
    address     TEXT        NOT NULL,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,

    CONSTRAINT email_confirmations_expires_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX email_confirmations_user_id_idx ON email_confirmations (user_id) WHERE used_at IS NULL;
CREATE INDEX email_confirmations_expires_at_idx ON email_confirmations (expires_at);

/*
The address somebody is waiting to prove.

Separate from the confirmed one on purpose. An account with a working address who
mistypes a new one would otherwise lose the working one the moment they pressed
save — and with it, the only way back in if they then forget the password.

Null when there is nothing outstanding, which is almost always.
*/
ALTER TABLE users
  ADD COLUMN pending_email TEXT;
