-- Assignees, labels, priorities and relationships.

-- name: AssignIssue :execrows
INSERT INTO issue_assignees (issue_id, user_id, assigned_by)
VALUES ($1, $2, $3)
ON CONFLICT (issue_id, user_id) DO NOTHING;

-- name: UnassignIssue :execrows
DELETE FROM issue_assignees WHERE issue_id = $1 AND user_id = $2;

-- name: ListIssueAssignees :many
SELECT users.id, users.username, users.display_name, issue_assignees.assigned_at
FROM issue_assignees
JOIN users ON users.id = issue_assignees.user_id
WHERE issue_assignees.issue_id = $1
ORDER BY users.username;

-- name: ListAssigneesForIssues :many
-- The assignees of a page of issues at once, rather than a query per card.
SELECT issue_assignees.issue_id, users.username, users.display_name
FROM issue_assignees
JOIN users ON users.id = issue_assignees.user_id
WHERE issue_assignees.issue_id = ANY(@issue_ids::uuid[])
ORDER BY users.username;

-- name: CreateProjectLabel :one
INSERT INTO project_labels (id, project_id, name, name_folded, colour)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListProjectLabels :many
SELECT * FROM project_labels WHERE project_id = $1 ORDER BY name_folded;

-- name: GetProjectLabel :one
SELECT * FROM project_labels WHERE id = $1;

-- name: UpdateProjectLabel :one
UPDATE project_labels SET name = $2, name_folded = $3, colour = $4
WHERE id = $1
RETURNING *;

-- name: ListIssueIDsWithLabel :many
SELECT issue_id FROM issue_labels WHERE label_id = $1;

-- name: DeleteProjectLabel :exec
DELETE FROM project_labels WHERE id = $1;

-- name: LabelIssue :execrows
INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UnlabelIssue :execrows
DELETE FROM issue_labels WHERE issue_id = $1 AND label_id = $2;

-- name: ListLabelsForIssues :many
SELECT issue_labels.issue_id, project_labels.id, project_labels.name, project_labels.colour
FROM issue_labels
JOIN project_labels ON project_labels.id = issue_labels.label_id
WHERE issue_labels.issue_id = ANY(@issue_ids::uuid[])
ORDER BY project_labels.name_folded;

-- name: CreateProjectPriority :one
INSERT INTO project_priorities (id, project_id, name, name_folded, rank)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListProjectPriorities :many
SELECT * FROM project_priorities WHERE project_id = $1 ORDER BY rank, created_at;

-- name: GetProjectPriority :one
SELECT * FROM project_priorities WHERE id = $1;

-- name: UpdateProjectPriority :one
UPDATE project_priorities SET name = $2, name_folded = $3
WHERE id = $1
RETURNING *;

-- name: SetProjectPriorityRank :exec
UPDATE project_priorities SET rank = $2 WHERE id = $1;

-- name: NextProjectPriorityRank :one
SELECT (COALESCE(MAX(rank), 0) + 1)::int FROM project_priorities WHERE project_id = $1;

-- name: ListIssueIDsWithPriority :many
SELECT id FROM issues WHERE priority_id = $1 ORDER BY number;

-- name: DeleteProjectPriority :exec
DELETE FROM project_priorities WHERE id = $1;

-- name: SetIssuePriority :one
UPDATE issues SET priority_id = $2, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: RelateIssues :execrows
-- Naming the conflict: the same relationship twice is nothing new, but a
-- second parent is a different constraint and must be refused, not swallowed.
INSERT INTO issue_relations (from_issue, to_issue, kind, created_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (from_issue, to_issue, kind) DO NOTHING;

-- name: UnrelateIssues :execrows
DELETE FROM issue_relations WHERE from_issue = $1 AND to_issue = $2 AND kind = $3;

-- name: LockProjectStream :exec
-- Holds a project's stream until the transaction ends, without taking a
-- position in it: the next write to the project waits here, which is what
-- lets a check against the project's current state stay true until the change
-- it justifies is written.
SELECT next_seq FROM room_sequences WHERE room_id = $1 FOR UPDATE;

-- name: ListProjectRelations :many
-- Every edge of one kind in a project, for walking the graph to refuse a
-- cycle. A project holds hundreds or a few thousand issues, so reading its
-- edges is cheap, and a walk in memory is simple enough to be obviously right.
SELECT r.from_issue, r.to_issue
FROM issue_relations r
JOIN issues i ON i.id = r.from_issue
WHERE i.project_id = @project_id AND r.kind = @kind;

-- name: ListRelationsOfIssue :many
-- An issue's relationships in both directions, with the other issue and its
-- state, for showing them.
SELECT r.from_issue, r.to_issue, r.kind,
       other.id AS other_id, other.number AS other_number, other.title AS other_title,
       s.name AS other_state, s.is_completion AS other_completed
FROM issue_relations r
JOIN issues other ON other.id = CASE WHEN r.from_issue = @issue_id THEN r.to_issue ELSE r.from_issue END
JOIN issue_states s ON s.id = other.state_id
WHERE (r.from_issue = @issue_id OR r.to_issue = @issue_id)
  AND other.deleted_at IS NULL
ORDER BY r.kind, other.number;

-- name: ListMyIssues :many
-- Everything assigned to somebody, in every project they may still see.
--
-- Visibility is joined, not filtered afterwards: an assignment in a private
-- project somebody has since left is never fetched, so it cannot be shown.
SELECT issues.*, projects.key AS project_key, projects.name AS project_name
FROM issue_assignees a
JOIN issues ON issues.id = a.issue_id
JOIN projects ON projects.id = issues.project_id
LEFT JOIN project_members pm ON pm.project_id = projects.id AND pm.user_id = a.user_id
WHERE a.user_id = @user_id
  AND issues.deleted_at IS NULL
  AND (projects.visibility = 'public' OR pm.user_id IS NOT NULL)
ORDER BY issues.updated_at DESC
LIMIT @page_size;
