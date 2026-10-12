/*
The names an account has given up.

A username is half of somebody's address, user@server, and the address is what
a profile link, a saved filter or an old conversation names them by. When it
changes, every one of those would otherwise point at nobody — or, worse, at
whoever registers the name next. So the old name is kept here: it resolves to
the person who held it for as long as nobody else holds it, and nobody else may
take it until the operator's holding period has passed.

One row per name given up, with when. The account's current name is not here;
it is on users, where the unique index that makes it theirs lives.
*/
CREATE TABLE username_history (
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    username        TEXT        NOT NULL,
    username_folded TEXT        NOT NULL,
    changed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Resolving an old name, and checking whether one is still held.
CREATE INDEX username_history_folded_idx ON username_history (username_folded, changed_at DESC);
-- An account's own history, newest first: the cooldown, and what an
-- administrator reads.
CREATE INDEX username_history_user_idx ON username_history (user_id, changed_at DESC);

/*
Whose a name is: the account that holds it now, or else the one that gave it
up most recently.

A function rather than the same subquery in every lookup, so "find a person by
name" means one thing wherever it is asked — a profile link, an assignee in a
saved filter, an enquiry — and an old link does not work in one place and
dangle in the next.
*/
CREATE FUNCTION user_named(folded TEXT) RETURNS UUID
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT id FROM users WHERE username_folded = folded),
        (SELECT user_id FROM username_history
          WHERE username_folded = folded
          ORDER BY changed_at DESC LIMIT 1)
    )
$$;
