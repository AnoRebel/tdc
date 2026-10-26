/*
A project whose stream an account's going away has left behind.

Deleting an account changes projects without recording anything in them: its
memberships and assignments cascade away, and what it created stops naming it.
Erasing a person also empties every event they sent, so the stream can no
longer be replayed past them. Either way the stream stops describing the
project — which is what stream_complete says, so the project is marked
incomplete and given a fresh baseline, its rows as they now stand, by the
server's next pass. Replaying starts from the latest baseline, so what the
erasure emptied is never needed again.

A trigger rather than a step in Go, because accounts are also deleted by hand,
against the database, which is how an operator carries out an erasure.

Which projects: those the person sent anything in, belonged to, or worked on.
Anything else that names them was put there by an event they sent.
*/
CREATE FUNCTION rebaseline_projects_of() RETURNS trigger AS $$
BEGIN
    UPDATE projects SET stream_complete = FALSE
    WHERE stream_complete
      AND (id IN (SELECT DISTINCT room_id FROM events WHERE sender_id = OLD.id)
        OR id IN (SELECT project_id FROM project_members WHERE user_id = OLD.id)
        OR id IN (SELECT issues.project_id FROM issue_assignees
                  JOIN issues ON issues.id = issue_assignees.issue_id
                  WHERE issue_assignees.user_id = OLD.id));
    RETURN OLD;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_rebaseline_projects BEFORE DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION rebaseline_projects_of();
