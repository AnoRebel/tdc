DROP TABLE IF EXISTS integration_context;
DROP TABLE IF EXISTS message_interactions;

DROP INDEX IF EXISTS action_presses_event_action_idx;
ALTER TABLE action_presses DROP CONSTRAINT IF EXISTS action_presses_once;

-- Presses against later versions collapse onto the original rule, which may
-- now find duplicates: keep the earliest of each.
DELETE FROM action_presses a
USING action_presses b
WHERE a.event_id = b.event_id
  AND a.user_id = b.user_id
  AND a.action = b.action
  AND a.created_at > b.created_at;

ALTER TABLE action_presses
    ADD CONSTRAINT action_presses_once UNIQUE (event_id, user_id, action);
CREATE INDEX action_presses_event_action_idx ON action_presses (event_id, action);

ALTER TABLE action_presses DROP COLUMN IF EXISTS value;
ALTER TABLE action_presses DROP COLUMN IF EXISTS version;
