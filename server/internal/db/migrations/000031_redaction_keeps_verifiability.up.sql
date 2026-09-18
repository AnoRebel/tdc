/*
The digest of an event's content exactly as it was signed.

Recorded for every event, not only redacted ones, and the reason is Postgres
rather than redaction. Content is stored as jsonb, which normalises it —
`{"b":1,"a":2}` comes back as `{"a": 2, "b": 1}`, reordered and respaced. Those
are different bytes, so an event read back from the database could never hash to
what was signed, and no stored event was verifiable at all.

Hashing at the moment of signing and keeping the digest fixes both that and
redaction at once: verification uses the digest rather than rehashing whatever
the database returned, so it no longer matters that jsonb tidied the content —
and an event whose content has been removed still has the digest.

It discloses nothing: recovering a message body from a SHA-256 means guessing
the body.

An event's identifier and signature are computed over its content. Removing the
content therefore removes the ability to reproduce either — and an event that
cannot reproduce its own identifier reads as tampered with, which is exactly the
thing the log exists to distinguish from a deliberate redaction.

So the content's digest is kept. It discloses nothing about what was said —
recovering a message body from a SHA-256 means guessing the body — while proving
that whatever was signed hashed to this. Ordering is untouched: the row, its
sequence and its neighbours are where they were.

Null on an event that still has its content, where the digest is computed from
the content itself and storing it would be storing what can be derived.
*/
ALTER TABLE events
  ADD COLUMN content_hash TEXT;

/*
Events written before this column existed cannot be made verifiable.

What was signed was the content before jsonb touched it, and that is gone — what
is stored is the normalised form, whose digest is a different value. Computing a
digest from it now would record something that was never signed, which is worse
than recording nothing: it would make an unverifiable event look verifiable.

So they are marked, and a verifier treats the marker as "this cannot be
checked" rather than silently accepting or silently failing. An operator asking
why an old event will not verify deserves that answer rather than silence.
*/
UPDATE events
SET content_hash = 'unrecorded'
WHERE content_hash IS NULL;
