/*
Where a peer actually is, as distinct from which peer it is.

`peer` is a host, because that is a server's identity: one on :8443 is the same
server as one on :443, and the trust lists compare them that way deliberately.
But an identity is not an address. Reaching a peer by assuming https on the
default port works in production and fails for every deployment behind another
port — including every local trial, which is where an operator first meets it.

So the address the operator gave is kept beside the host derived from it. The
host decides who the peer is; the address decides where to send.
*/
ALTER TABLE room_peers ADD COLUMN address TEXT NOT NULL DEFAULT '';

/*
Existing rows get https on the default port, which is what the code assumed
before this column existed. Wrong for a peer on another port, and no more wrong
than it already was — a migration cannot recover an address nobody recorded.
*/
UPDATE room_peers SET address = 'https://' || peer WHERE address = '';
