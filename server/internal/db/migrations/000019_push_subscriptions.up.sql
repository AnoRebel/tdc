-- Where a person's browser can be reached when the application is closed.
--
-- Per device rather than per person: somebody signed in on a phone and a
-- laptop has two, and revoking one must not silence the other. A row here is
-- an endpoint at somebody else's push service plus the keys that encrypt a
-- payload to it — this server never holds anything that could decrypt one.
CREATE TABLE push_subscriptions (
    id         UUID PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The push service's own address for this device. Unique because a browser
    -- resubscribing returns the same endpoint, and two rows for one device
    -- would send every notification twice.
    endpoint   TEXT NOT NULL UNIQUE,

    -- The keys the payload is encrypted to. Public halves only: the private
    -- ones never leave the browser, which is what stops this server — or
    -- anybody who reaches its database — reading a notification it sent.
    p256dh_key TEXT NOT NULL,
    auth_key   TEXT NOT NULL,

    -- What subscribed, so somebody reviewing their devices sees "Firefox on
    -- Android" rather than an opaque endpoint.
    user_agent TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- When a push was last delivered, so a subscription nothing has reached in
    -- a long time can be recognised as stale.
    last_used_at TIMESTAMPTZ,

    -- How many times in a row delivery has failed without the service saying
    -- the subscription is gone. Cleared on success.
    failures   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);

-- Whether somebody wants message content in a notification their phone shows
-- on a lock screen.
--
-- On the account rather than per device: somebody who does not want their
-- messages on a lock screen does not want them on one of their lock screens.
ALTER TABLE users ADD COLUMN hide_push_content BOOLEAN NOT NULL DEFAULT false;
