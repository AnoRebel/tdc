/*
A code sent by mail, for the sign-in it was sent for.

On the pending sign-in rather than in the credentials table, because it is not a
credential: it exists for one attempt, for minutes, and is worth nothing
afterwards. A TOTP secret is a thing somebody has; this is a thing somebody was
just sent.

Hashed, like every other bearer value here — a database read should not yield a
code that gets anybody in.

Null until somebody asks for one. Most sign-ins never do: an emailed code is the
fallback for an account whose only other factor is unreachable, and it is the
weakest of the three because it is only as good as the mailbox.
*/
ALTER TABLE pending_sign_ins
  ADD COLUMN emailed_code_hash BYTEA,
  /*
    When it stops working, separately from the ticket.

    Shorter than the ticket's own life: a ticket lasts long enough to find a
    phone and open an app, and a code that arrived by mail should not still work
    ten minutes after somebody read it over somebody's shoulder.
  */
  ADD COLUMN emailed_code_expires_at TIMESTAMPTZ;
