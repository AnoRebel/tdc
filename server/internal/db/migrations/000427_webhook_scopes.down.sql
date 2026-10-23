DROP INDEX IF EXISTS webhook_subscriptions_server_idx;
DELETE FROM webhook_subscriptions WHERE scope = 'server';
ALTER TABLE webhook_subscriptions DROP CONSTRAINT IF EXISTS webhook_subscriptions_scope;
ALTER TABLE webhook_subscriptions DROP COLUMN IF EXISTS scope;
ALTER TABLE webhook_subscriptions ALTER COLUMN room_id SET NOT NULL;
