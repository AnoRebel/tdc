DELETE FROM notifications WHERE reason = 'assigned';
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN ('mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin'));

DROP TABLE IF EXISTS issue_relations;
ALTER TABLE issues DROP COLUMN IF EXISTS priority_id;
DROP TABLE IF EXISTS project_priorities;
DROP TABLE IF EXISTS issue_labels;
DROP TABLE IF EXISTS project_labels;
DROP TABLE IF EXISTS issue_assignees;
