/*
A sign-in that has passed the first factor and not yet the second.

Between the password being right and a session existing, there is a moment that
has to be remembered: this person proved they know the password, and now owes a
code. Something must hold that, and it must not be the client — a client that
told the server "I already did the first part" would have skipped it.

Deliberately not a session. A session is the thing being earned, and issuing one
that only half works would mean every route deciding whether this particular
session is allowed yet. This is a separate, shorter-lived row that buys nothing
except the right to present a second factor.
*/
CREATE TABLE pending_sign_ins (
    id           UUID        PRIMARY KEY,

    -- The ticket, hashed. A bearer value for the minutes it lives, so it is
    -- stored the way every other one here is.
    ticket_hash  BYTEA       NOT NULL UNIQUE,

    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    /*
      What was asked of them, as the device that started it.

      Carried through so the session, when it is finally issued, records where
      the sign-in came from rather than where the second request did — which on
      a phone switching networks is not the same place.
    */
    user_agent   TEXT        NOT NULL DEFAULT '',
    ip_address   INET,

    /*
      How many codes have been tried against this ticket.

      A second factor is six digits, and a ticket that accepted unlimited
      attempts would reduce it to a matter of patience. Counted here rather
      than by address, because the ticket is what identifies the attempt and an
      attacker holding one can change where they are calling from.
    */
    attempts     INTEGER     NOT NULL DEFAULT 0,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL,

    CONSTRAINT pending_sign_ins_expires_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX pending_sign_ins_expires_at_idx ON pending_sign_ins (expires_at);
