-- Projects, their membership, their states and their issues.

-- name: CreateProject :one
INSERT INTO projects (id, key, key_folded, name, description, visibility, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;

-- name: GetProjectByKey :one
SELECT * FROM projects WHERE key_folded = $1;

-- name: ListProjectsForUser :many
-- The projects somebody may see: every public one, and the private ones they
-- are in. A private project they are not in never appears, which is what keeps
-- its existence undisclosed — the same rule as ListRoomsForUser.
SELECT projects.*, COALESCE(project_members.role, '')::text AS my_role
FROM projects
LEFT JOIN project_members
       ON project_members.project_id = projects.id
      AND project_members.user_id = $1
WHERE projects.visibility = 'public'
   OR project_members.user_id IS NOT NULL
ORDER BY projects.name;

-- name: UpdateProject :one
UPDATE projects
SET name = $2, description = $3, visibility = $4, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AllocateIssueNumber :one
-- The next issue number, under the project row's lock, so two people filing at
-- once cannot both be given the same one.
UPDATE projects
SET next_issue_number = next_issue_number + 1
WHERE id = $1
RETURNING (next_issue_number - 1)::bigint;

-- name: AddProjectMember :exec
INSERT INTO project_members (project_id, user_id, role, added_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveProjectMember :exec
DELETE FROM project_members WHERE project_id = $1 AND user_id = $2;

-- name: GetProjectMemberRole :one
SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2;

-- name: ListProjectMembers :many
SELECT users.id, users.username, users.display_name, project_members.role, project_members.joined_at
FROM project_members
JOIN users ON users.id = project_members.user_id
WHERE project_members.project_id = $1
ORDER BY project_members.role, users.username;

-- name: CountProjectAdmins :one
SELECT count(*) FROM project_members WHERE project_id = $1 AND role = 'admin';

-- name: CreateIssueState :one
INSERT INTO issue_states (id, project_id, name, name_folded, position, is_completion)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListIssueStates :many
SELECT * FROM issue_states WHERE project_id = $1 ORDER BY position, created_at;

-- name: GetIssueState :one
SELECT * FROM issue_states WHERE id = $1;

-- name: UpdateIssueState :one
UPDATE issue_states
SET name = $2, name_folded = $3, is_completion = $4
WHERE id = $1
RETURNING *;

-- name: SetIssueStatePosition :exec
UPDATE issue_states SET position = $2 WHERE id = $1;

-- name: NextIssueStatePosition :one
SELECT (COALESCE(MAX(position), 0) + 1)::int FROM issue_states WHERE project_id = $1;

-- name: DeleteIssueState :exec
DELETE FROM issue_states WHERE id = $1;

-- name: ListIssueIDsInState :many
-- Every issue in a state, deleted ones included: a deleted issue still names
-- its state, and a state cannot go while anything names it.
SELECT id FROM issues WHERE state_id = $1 ORDER BY number;

-- name: CreateIssue :one
INSERT INTO issues (id, project_id, number, title, description, state_id, author_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetIssueByID :one
SELECT * FROM issues WHERE id = $1;

-- name: GetIssueByNumber :one
SELECT * FROM issues WHERE project_id = $1 AND number = $2;

-- name: LockIssue :one
-- Reads an issue and holds it until the transaction ends, so a change and the
-- event recording it are decided against a value nobody else is changing.
SELECT * FROM issues WHERE id = $1 FOR UPDATE;

-- name: TransitionIssue :one
-- Moves an issue between states, only if it is still in the state the mover
-- saw. Two people moving the same card at once both reach this statement; the
-- first changes the row and the second matches nothing, so it is told what the
-- state became rather than silently overwriting it.
UPDATE issues
SET state_id = @to_state, updated_at = NOW()
WHERE id = @id AND state_id = @from_state
RETURNING *;

-- name: SetIssueTitle :one
UPDATE issues SET title = $2, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: SetIssueDescription :one
UPDATE issues SET description = $2, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: MarkIssueDeleted :one
UPDATE issues SET deleted_at = NOW(), deleted_by = $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: ListProjectIssues :many
-- A page of a project's issues, newest first.
--
-- Callers check the project is visible to the person asking before calling
-- this; the rows themselves carry nothing a person who can see the project
-- could not see.
--
-- Filters narrow together: an issue must match every one given. Sorting by
-- priority is by the project's own ranks, most urgent first, with issues that
-- have no priority after every issue that has one.
SELECT issues.*
FROM issues
LEFT JOIN project_priorities p ON p.id = issues.priority_id
WHERE issues.project_id = @project_id
  AND issues.deleted_at IS NULL
  AND (sqlc.narg('state_id')::uuid IS NULL OR issues.state_id = sqlc.narg('state_id')::uuid)
  AND (sqlc.narg('priority_id')::uuid IS NULL OR issues.priority_id = sqlc.narg('priority_id')::uuid)
  AND (sqlc.narg('label_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM issue_labels l
        WHERE l.issue_id = issues.id AND l.label_id = sqlc.narg('label_id')::uuid))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM issue_assignees a
        WHERE a.issue_id = issues.id AND a.user_id = sqlc.narg('assignee_id')::uuid))
  AND (@before_number::bigint = 0 OR issues.number < @before_number::bigint)
ORDER BY
  CASE WHEN @by_priority::boolean THEN COALESCE(p.rank, 2147483647) END,
  issues.number DESC
LIMIT @page_size;

-- name: ListIssueEvents :many
-- One issue's history: its project's stream, filtered to it. The partial
-- expression index events_issue_idx serves exactly this.
SELECT * FROM events
WHERE room_id = @project_id
  AND type LIKE 'issue.%'
  AND content->>'issue' = @issue_id::text
ORDER BY sequence;

-- name: ListProjectStream :many
-- A project's stream in order, for replaying it.
SELECT * FROM events
WHERE room_id = @project_id AND sequence > @after_sequence
ORDER BY sequence
LIMIT @page_size;

-- name: SetProjectFederated :execrows
-- Rows changed, so opening an already opened project records nothing.
UPDATE projects SET federated = $2, updated_at = NOW() WHERE id = $1 AND federated <> $2;

-- name: AddProjectRemoteMember :exec
INSERT INTO project_remote_members (project_id, address, home_server, role, added_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (project_id, address) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveProjectRemoteMember :exec
DELETE FROM project_remote_members WHERE project_id = $1 AND address = $2;

-- name: GetProjectRemoteMemberRole :one
SELECT role FROM project_remote_members WHERE project_id = $1 AND address = $2;

-- name: ListProjectRemoteMembers :many
SELECT address, role, joined_at FROM project_remote_members
WHERE project_id = $1
ORDER BY role, address;
