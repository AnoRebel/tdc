-- A project's rows as its stream describes them, and the stream's own pages.
--
-- People are named here by username, never by account: the stream names them
-- by address, and a projection rebuilt on another server has accounts of its
-- own. Somebody since removed from the server is the empty string, which is
-- what the column holds once their account has gone.

-- name: GetProjectForStream :one
SELECT projects.*, COALESCE(creator.username, '')::text AS created_by_username
FROM projects
LEFT JOIN users creator ON creator.id = projects.created_by
WHERE projects.id = $1;

-- name: ListProjectMembersForStream :many
SELECT member.username, project_members.role, COALESCE(adder.username, '')::text AS added_by_username
FROM project_members
JOIN users member ON member.id = project_members.user_id
LEFT JOIN users adder ON adder.id = project_members.added_by
WHERE project_members.project_id = $1;

-- name: ListProjectRemoteMembersForStream :many
SELECT project_remote_members.address, project_remote_members.role, COALESCE(adder.username, '')::text AS added_by_username
FROM project_remote_members
LEFT JOIN users adder ON adder.id = project_remote_members.added_by
WHERE project_remote_members.project_id = $1;

-- name: ListIssuesForStream :many
-- Deleted issues included: a deleted issue is still a row, still numbered.
SELECT issues.id, issues.number, issues.title, issues.description, issues.state_id,
       issues.priority_id, (issues.deleted_at IS NOT NULL)::bool AS deleted,
       COALESCE(author.username, '')::text AS author_username,
       COALESCE(deleter.username, '')::text AS deleted_by_username
FROM issues
LEFT JOIN users author ON author.id = issues.author_id
LEFT JOIN users deleter ON deleter.id = issues.deleted_by
WHERE issues.project_id = $1;

-- name: ListIssueAssigneesForStream :many
SELECT issue_assignees.issue_id, assignee.username,
       COALESCE(assigner.username, '')::text AS assigned_by_username
FROM issue_assignees
JOIN issues ON issues.id = issue_assignees.issue_id
JOIN users assignee ON assignee.id = issue_assignees.user_id
LEFT JOIN users assigner ON assigner.id = issue_assignees.assigned_by
WHERE issues.project_id = $1;

-- name: ListIssueLabelsForStream :many
SELECT issue_labels.issue_id, issue_labels.label_id
FROM issue_labels
JOIN issues ON issues.id = issue_labels.issue_id
WHERE issues.project_id = $1;

-- name: ListIssueRelationsForStream :many
SELECT issue_relations.from_issue, issue_relations.to_issue, issue_relations.kind,
       COALESCE(relater.username, '')::text AS created_by_username
FROM issue_relations
JOIN issues ON issues.id = issue_relations.from_issue
LEFT JOIN users relater ON relater.id = issue_relations.created_by
WHERE issues.project_id = $1;

-- name: ListCyclesForStream :many
SELECT cycles.*, COALESCE(creator.username, '')::text AS created_by_username
FROM cycles
LEFT JOIN users creator ON creator.id = cycles.created_by
WHERE cycles.project_id = $1;

-- name: ListMilestonesForStream :many
SELECT milestones.*, COALESCE(creator.username, '')::text AS created_by_username
FROM milestones
LEFT JOIN users creator ON creator.id = milestones.created_by
WHERE milestones.project_id = $1;

-- name: ListIssueCyclesForStream :many
SELECT issue_cycle.issue_id, issue_cycle.cycle_id
FROM issue_cycle
JOIN issues ON issues.id = issue_cycle.issue_id
WHERE issues.project_id = $1;

-- name: ListIssueMilestonesForStream :many
SELECT issue_milestone.issue_id, issue_milestone.milestone_id
FROM issue_milestone
JOIN issues ON issues.id = issue_milestone.issue_id
WHERE issues.project_id = $1;

-- name: ListProjectsWithIncompleteStreams :many
SELECT id FROM projects WHERE NOT stream_complete ORDER BY created_at;

-- name: MarkProjectStreamComplete :execrows
-- Only once: the second server to try finds the row already marked, and
-- appends nothing.
UPDATE projects SET stream_complete = TRUE WHERE id = $1 AND NOT stream_complete;

-- name: ListProjectHistory :many
-- What happened to a project itself, rather than to one of its issues, newest
-- first, a page at a time before a position in its stream.
SELECT * FROM events
WHERE room_id = @project_id
  AND type LIKE 'project.%'
  AND (@before_sequence::bigint = 0 OR sequence < @before_sequence::bigint)
ORDER BY sequence DESC
LIMIT @page_size;
