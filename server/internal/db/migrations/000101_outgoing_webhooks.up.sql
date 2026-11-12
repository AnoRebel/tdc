/*
Outgoing webhooks: somebody's endpoint told about events as they happen.

Three tables, each an answer to a question an owner or an operator asks with
psql as readily as with the interface: what is subscribed to what, what is
waiting to be sent, and what happened each time something was tried.
*/

/*
A subscription: one endpoint, one channel, some event types.

Bound to a channel rather than to "everything its owner can see", so what it
receives is something its owner chose rather than something that grows every
time they join a channel. The owner's access is checked on every delivery
rather than when the subscription was made, which is what makes losing access
to the channel stop the deliveries.
*/
CREATE TABLE webhook_subscriptions (
    id                   UUID        PRIMARY KEY,
    owner_id             UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id              UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    label                TEXT        NOT NULL,
    url                  TEXT        NOT NULL,
    /*
      The shared secret the HMAC is keyed with.

      Stored as it is, because it is needed to sign every delivery: it cannot be
      hashed the way a credential presented to us is. It authorises nothing
      here — nobody can use it to act on this server — which is why it can be
      kept, and why it is shown to its owner once and never again.
    */
    secret               TEXT        NOT NULL,
    event_types          TEXT[]      NOT NULL,
    -- The payload version the subscription was made against, which it keeps
    -- receiving after a newer version exists.
    payload_version      INTEGER     NOT NULL,
    state                TEXT        NOT NULL DEFAULT 'active',
    disabled_at          TIMESTAMPTZ,
    disabled_reason      TEXT,
    -- Failed attempts since the last success, across every delivery.
    consecutive_failures INTEGER     NOT NULL DEFAULT 0,
    -- How often a delivery was held back by the rate limit, and when last,
    -- so an owner can see that it happened rather than infer it from delays.
    rate_limited_count   BIGINT      NOT NULL DEFAULT 0,
    last_rate_limited_at TIMESTAMPTZ,
    -- When a delivery last found the owner unable to read the channel.
    access_lost_at       TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT webhook_subscriptions_state CHECK (state IN ('active', 'disabled')),
    CONSTRAINT webhook_subscriptions_label CHECK (length(trim(label)) > 0),
    CONSTRAINT webhook_subscriptions_types CHECK (cardinality(event_types) > 0)
);

CREATE INDEX webhook_subscriptions_room_idx ON webhook_subscriptions (room_id) WHERE state = 'active';
CREATE INDEX webhook_subscriptions_owner_idx ON webhook_subscriptions (owner_id);

/*
One thing to send, and where it stands.

The payload is rendered once, when the event happens, and kept as bytes: a
retry sends exactly what the first attempt sent, and JSONB would normalise it
into different bytes than the ones a receiver deduplicates or a test compares.
It is signed afresh at each attempt, so a retry an hour later still arrives
inside its own replay window.
*/
CREATE TABLE webhook_deliveries (
    id              UUID        PRIMARY KEY,
    /*
      Where it is going, as a name: "webhook:<id>" for a subscription's
      deliveries. Each target has at most one delivery in flight, which is the
      rule that keeps a slow receiver to one worker — so it is keyed on this
      rather than on the subscription, and applies to every kind of delivery
      the queue carries.
    */
    target          TEXT        NOT NULL,
    subscription_id UUID        REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id        TEXT        REFERENCES events(id) ON DELETE CASCADE,
    kind            TEXT        NOT NULL,
    payload         BYTEA       NOT NULL,
    state           TEXT        NOT NULL DEFAULT 'pending',
    attempts        INTEGER     NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,

    /*
      pending    — waiting for its next attempt
      delivering — claimed by a worker right now
      delivered  — a receiver answered 2xx
      abandoned  — every attempt failed; the documented limit was reached
      cancelled  — not sent, because the subscription was disabled or its
                   owner could no longer read the channel
    */
    CONSTRAINT webhook_deliveries_state CHECK (
        state IN ('pending', 'delivering', 'delivered', 'abandoned', 'cancelled'))
);

-- What a worker claims: the oldest due delivery.
CREATE INDEX webhook_deliveries_due_idx ON webhook_deliveries (next_attempt_at, created_at)
    WHERE state = 'pending';
-- What is in flight, and to where, checked before claiming more.
CREATE INDEX webhook_deliveries_in_flight_idx ON webhook_deliveries (target)
    WHERE state = 'delivering';
CREATE INDEX webhook_deliveries_subscription_idx ON webhook_deliveries (subscription_id, created_at DESC);
-- Pruning by age.
CREATE INDEX webhook_deliveries_created_idx ON webhook_deliveries (created_at);

/*
Every attempt, and what came of it.

Kept separately from the delivery so the history shows each try — its status,
what the receiver said, how long it took — rather than only the last.
*/
CREATE TABLE webhook_attempts (
    id           UUID        PRIMARY KEY,
    delivery_id  UUID        NOT NULL REFERENCES webhook_deliveries(id) ON DELETE CASCADE,
    attempt      INTEGER     NOT NULL,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- delivered, failed, or rate_limited (held back, and not counted as a try).
    outcome      TEXT        NOT NULL,
    status_code  INTEGER,
    error        TEXT,
    response     TEXT,
    duration_ms  INTEGER     NOT NULL DEFAULT 0,

    CONSTRAINT webhook_attempts_outcome CHECK (outcome IN ('delivered', 'failed', 'rate_limited'))
);

CREATE INDEX webhook_attempts_delivery_idx ON webhook_attempts (delivery_id, attempted_at);

/*
A subscription that was turned off tells its owner, through the notifications
everybody already reads, rather than a mechanism of its own.
*/
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN (
        'mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin',
        -- An outgoing webhook failed for long enough to be turned off.
        'webhook_disabled'
    ));
