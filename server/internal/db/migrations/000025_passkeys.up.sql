/*
What a passkey is, beyond what a credential row already holds.

The credentials table was built for this: kind 'passkey', the authenticator's
credential ID in external_id, its public key in secret, and the signature
counter in sign_count. Those four are enough to verify an assertion, and they
are where they already were.

What they are not enough for is everything else WebAuthn has grown. A credential
carries its transports, its attestation, its flags — whether it is backed up,
whether the authenticator can do user verification — and the library's own
structure gains fields as the specification does. A column for each would mean a
migration every time that happens, to store something this server passes through
without reading.

So the rest is JSON. It is written whole by one package and read whole by the
same one; the database never looks inside it, and the parts the database does
index — the credential ID, the counter — stay in columns where they were.
*/
ALTER TABLE credentials
  ADD COLUMN passkey JSONB;

/*
An object or nothing.

A passkey row must carry one and a password row must not, which is the kind of
thing that is true on the day it is written and quietly stops being true later.
Stating it here means a bug that writes the wrong shape is refused at the
insert rather than found at the sign-in it breaks.
*/
ALTER TABLE credentials
  ADD CONSTRAINT credentials_passkey_shape
    CHECK (
      (kind = 'passkey' AND passkey IS NOT NULL AND jsonb_typeof(passkey) = 'object')
      OR (kind <> 'passkey' AND passkey IS NULL)
    );

/*
Who an authenticator says it is signing in.

WebAuthn's user handle: a stable, opaque, unguessable value the authenticator
stores alongside the credential and gives back at sign-in. It is what makes a
passkey sign-in possible with nothing typed — the authenticator offers the
account, rather than somebody naming it first.

Deliberately not the account's id. That id appears in URLs, in API responses and
in the audit log, and the specification asks for a value that cannot be
correlated with anything else; one that is already public correlates with
everything.

Null until somebody enrols their first passkey, so an account that never does
carries nothing.
*/
ALTER TABLE users
  ADD COLUMN webauthn_handle BYTEA UNIQUE;
