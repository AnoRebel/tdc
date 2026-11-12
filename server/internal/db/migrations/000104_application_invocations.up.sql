/*
Invocations: an application told about a press on its message at an address of
its own, rather than only through a live connection it has to hold open.

The address and the secret its deliveries are signed with live on the
application, set by whoever manages it. The deliveries themselves go through
the same queue as a webhook's — the same signing, the same retries, the same
one-in-flight rule per destination — so an application that cannot be reached
holds one worker and never the pool, exactly as a failing webhook does.
*/
ALTER TABLE applications
    ADD COLUMN invocation_url    TEXT NOT NULL DEFAULT '',
    /*
      Kept as it is, like a webhook's: it signs every delivery, so it cannot be
      hashed. It authorises nothing on this server, and is shown once.
    */
    ADD COLUMN invocation_secret TEXT NOT NULL DEFAULT '';

/*
An invocation is a delivery whose destination is an application rather than a
subscription. Its target is "application:<id>", so the one-in-flight rule holds
per application as it does per subscription.
*/
ALTER TABLE webhook_deliveries
    ADD COLUMN application_id UUID REFERENCES applications(id) ON DELETE CASCADE;

CREATE INDEX webhook_deliveries_application_idx ON webhook_deliveries (application_id, created_at DESC)
    WHERE application_id IS NOT NULL;
