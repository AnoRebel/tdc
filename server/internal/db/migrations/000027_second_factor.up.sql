/*
A second thing somebody must have, beyond the one they know.

Two new kinds of credential, in the table that already holds the rest. A
password proves knowledge and a passkey proves possession; what these add is a
way to require both of a password sign-in, for somebody who wants that or an
operator who requires it.

'totp'      — a shared secret an authenticator app turns into a six-digit code.
              One per account: the app is the factor, and a second secret would
              be a second app to keep in step rather than more security.
'recovery'  — a single-use code, issued in a batch, for the day the phone is
              lost. Several per account, each usable once.
*/
ALTER TABLE credentials
  DROP CONSTRAINT credentials_kind_valid;

ALTER TABLE credentials
  ADD CONSTRAINT credentials_kind_valid
    CHECK (kind IN ('password', 'passkey', 'oauth2', 'totp', 'recovery'));

/*
At most one TOTP secret per account.

The same shape as the one-password rule above it, and for the same reason: two
would be two apps that both work, where somebody removing the one they know
about would still be carrying a second factor they had forgotten.
*/
CREATE UNIQUE INDEX credentials_one_totp_per_user_idx
  ON credentials (user_id)
  WHERE kind = 'totp';

/*
A recovery code, once used, is used.

Not deleted: the row stays so that "you have three codes left" is answerable and
so somebody can see that one was spent. Spending it is setting this, and the
query that spends one requires it to be null, which is what makes it single-use
under concurrent attempts rather than merely by convention.
*/
ALTER TABLE credentials
  ADD COLUMN used_at TIMESTAMPTZ;

/*
Which TOTP code was last accepted, as a counter of thirty-second periods.

A code is valid for its whole window, so without this the same six digits work
repeatedly for up to a minute and a half — long enough for somebody who read
them over a shoulder, or off a phishing page, to use them again. Recording the
period the last accepted code belonged to, and refusing anything not later than
it, makes each code good once.

Null until the first code is accepted.
*/
ALTER TABLE credentials
  ADD COLUMN last_period BIGINT;

/*
Whether somebody has finished turning a second factor on.

Enrolling TOTP is two steps: the server issues a secret, and the person proves
their app has it by entering a code. Between those, a row exists that must not
yet be required at sign-in — otherwise a scan that went wrong would lock
somebody out of their own account.

Recovery codes are confirmed the moment they are issued, since there is nothing
to prove.
*/
ALTER TABLE credentials
  ADD COLUMN confirmed_at TIMESTAMPTZ;
