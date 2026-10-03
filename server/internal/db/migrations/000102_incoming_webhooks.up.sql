/*
Incoming webhooks: an address an outside system posts into one channel through.

Its own table rather than an API token, because it is a different thing. A
token acts as an account, with that account's permissions intersected on every
request; an incoming webhook acts as nobody, can do exactly one thing — post in
the channel it was made for — and keeps doing it whatever happens to the person
who made it. Modelling it as a token would mean inventing an account to hold it,
or weakening what a token promises for this one case.

The credential is expected to leak — it is pasted into other services by
design — so everything here assumes it has: it is stored hashed, bound to one
channel, checked without any cache on every call so a revocation is final at
once, and every post through it is attributed to it rather than to anybody.
*/
CREATE TABLE incoming_webhooks (
    id              UUID        PRIMARY KEY,
    room_id         UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    label           TEXT        NOT NULL,
    -- SHA-256 of the credential. The credential itself is shown once.
    token_hash      BYTEA       NOT NULL UNIQUE,
    created_by      UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ,
    revoked_by      UUID        REFERENCES users(id) ON DELETE SET NULL,
    last_used_at    TIMESTAMPTZ,
    posted_count    BIGINT      NOT NULL DEFAULT 0,
    -- Calls refused by the rate limit, so an owner can see that it happened
    -- rather than find messages missing.
    refused_count   BIGINT      NOT NULL DEFAULT 0,
    last_refused_at TIMESTAMPTZ,

    CONSTRAINT incoming_webhooks_label CHECK (length(trim(label)) > 0)
);

CREATE INDEX incoming_webhooks_room_idx ON incoming_webhooks (room_id, created_at DESC);
