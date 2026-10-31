-- Who assigned somebody on another server, as issue_assignees records for
-- somebody here: a person of this server, or nobody.
ALTER TABLE issue_remote_assignees
    ADD COLUMN assigned_by UUID REFERENCES users(id) ON DELETE SET NULL;
