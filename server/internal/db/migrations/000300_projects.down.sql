-- Grants of permissions this schema no longer has, at any scope.
DELETE FROM role_permissions WHERE permission LIKE 'project.%' OR permission LIKE 'issue.%';

DROP INDEX IF EXISTS events_issue_idx;
DROP TABLE IF EXISTS issues;
DROP TABLE IF EXISTS issue_states;
DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS projects;

-- The rooms that were projects' streams and issues' discussions go too: a
-- room of a kind the constraint no longer admits would stop it being restored.
DELETE FROM rooms WHERE kind IN ('project', 'issue-discussion');
ALTER TABLE rooms DROP CONSTRAINT rooms_kind_valid;
ALTER TABLE rooms ADD CONSTRAINT rooms_kind_valid
    CHECK (kind IN ('channel', 'direct', 'disposable'));
