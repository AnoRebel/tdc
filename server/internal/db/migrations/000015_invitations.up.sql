-- Invitations: a link or a short code that grants membership of one thing.
--
-- Two kinds in one table. A channel invitation admits somebody to that channel;
-- a server invitation admits them to the server under a policy that requires
-- one. They share every property that matters — expiry, use limit, binding,
-- revocation — so splitting them would mean maintaining those rules twice and
-- getting them different.

CREATE TABLE invitations (
    id          UUID        PRIMARY KEY,

    /*
      The secret, stored as a hash.

      An invitation is a bearer credential: whoever holds it can use it. A
      database read should therefore not yield working invitations, which is
      the same reason a password is not stored either. The lookup is by hash,
      so the plaintext exists only in the link somebody was sent.
    */
    token_hash  BYTEA       NOT NULL UNIQUE,

    /*
      A short code, for reading aloud or typing on a phone.

      Also hashed, and separate from the token: an invitation may be issued as
      a link, a code, or both, and the two are used through different paths
      with different rate limits — a code is short enough to guess at, a link
      is not.
    */
    code_hash   BYTEA       UNIQUE,

    -- What it admits somebody to. Null room means the server itself.
    room_id     UUID        REFERENCES rooms(id) ON DELETE CASCADE,

    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    -- Kept as an address so an audit trail survives the account being removed.
    created_by_address TEXT NOT NULL,

    /*
      Optional binding to one person.

      An invitation meant for a particular person should not work for whoever
      they forward it to. Held as an email address rather than a user
      identifier because the person being invited usually has no account yet —
      that is what they are being invited to create.
    */
    bound_to    TEXT,

    -- How many times it may be accepted. Null is unlimited, which is what an
    -- open "join our channel" link is.
    max_uses    INTEGER,
    uses        INTEGER     NOT NULL DEFAULT 0,

    -- When it stops working. Null never expires, which an operator may want
    -- for a permanent join link on a public channel.
    expires_at  TIMESTAMPTZ,

    /*
      When it was revoked.

      A column rather than a deletion, so that revoking is auditable and so a
      revoked invitation is refused by the same code path as an expired one —
      which is what makes the two indistinguishable to somebody probing.
    */
    revoked_at  TIMESTAMPTZ,
    revoked_by  UUID        REFERENCES users(id) ON DELETE SET NULL,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT invitations_uses_sane CHECK (uses >= 0),
    CONSTRAINT invitations_max_uses_sane CHECK (max_uses IS NULL OR max_uses > 0)
);

-- Listing what a channel has outstanding, for whoever manages it.
CREATE INDEX invitations_room_idx ON invitations (room_id, created_at DESC);

-- Server invitations, which have no room.
CREATE INDEX invitations_server_idx ON invitations (created_at DESC)
    WHERE room_id IS NULL;

/*
  Who has accepted what.

  Separate from the use count because the count is a limit and this is a
  record: an audit asks who joined through which invitation, and a number
  cannot answer that. It also stops one person consuming several uses of an
  invitation by accepting it repeatedly.
*/
CREATE TABLE invitation_uses (
    invitation_id UUID      NOT NULL REFERENCES invitations(id) ON DELETE CASCADE,
    user_id       UUID      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    used_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (invitation_id, user_id)
);
