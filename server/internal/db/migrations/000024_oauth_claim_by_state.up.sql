/*
Which sign-in an outcome belongs to.

The web client is handed its outcome directly: the callback redirects the same
browser that started the sign-in, carrying the handle in the fragment. The
desktop client has no such browser. It opens the system one and stays where it
is, so the callback resolves somewhere it cannot see and the redirect lands in a
web page it is not showing.

What the desktop does hold is the state value it was given when the sign-in
began. Recording it here — hashed, as it is everywhere else — lets that client
ask for the outcome of its own sign-in and nobody else's.

Null for a sign-in nobody claims this way, which is every web sign-in.
*/
ALTER TABLE oauth_outcomes
  ADD COLUMN state_hash BYTEA;

CREATE UNIQUE INDEX oauth_outcomes_state_hash_idx
  ON oauth_outcomes (state_hash)
  WHERE state_hash IS NOT NULL;
