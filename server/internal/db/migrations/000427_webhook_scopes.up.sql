/*
What a subscription listens to: one channel, one project, or the whole server.

A project's history is a room's log, so a project subscription names the
project's stream as its room, exactly as a channel subscription names the
channel. A server subscription names no room at all: it hears what happens in
every room its owner can read, and the events that belong to no room — a new
account, a change of role — which are each guarded by a permission of their own.
*/
ALTER TABLE webhook_subscriptions ALTER COLUMN room_id DROP NOT NULL;
ALTER TABLE webhook_subscriptions ADD COLUMN scope TEXT NOT NULL DEFAULT 'channel';
ALTER TABLE webhook_subscriptions ADD CONSTRAINT webhook_subscriptions_scope
    CHECK (scope IN ('channel', 'project', 'server') AND ((scope = 'server') = (room_id IS NULL)));

CREATE INDEX webhook_subscriptions_server_idx ON webhook_subscriptions (created_at)
    WHERE state = 'active' AND scope = 'server';
