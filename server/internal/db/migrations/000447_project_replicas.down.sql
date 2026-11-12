DROP TABLE IF EXISTS project_replicas;
DROP TABLE IF EXISTS issue_remote_assignees;
ALTER TABLE issues DROP COLUMN IF EXISTS author_address;
ALTER TABLE projects DROP COLUMN IF EXISTS replica_error;
ALTER TABLE projects DROP CONSTRAINT projects_key_shape;
ALTER TABLE projects ADD CONSTRAINT projects_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$');
