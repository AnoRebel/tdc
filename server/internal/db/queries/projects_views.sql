-- Saved views.

-- name: CreateProjectView :one
INSERT INTO project_views (id, project_id, owner_id, name, layout, filter, shared)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetProjectView :one
SELECT * FROM project_views WHERE id = $1;

-- name: UpdateProjectView :one
UPDATE project_views
SET name = $2, layout = $3, filter = $4, shared = $5, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteProjectView :exec
DELETE FROM project_views WHERE id = $1;

-- name: ListViewsOfProject :many
-- A project's views a person may use: their own, and those shared. The caller
-- has already established that they may see the project.
SELECT project_views.*, users.username AS owner_username
FROM project_views
JOIN users ON users.id = project_views.owner_id
WHERE project_views.project_id = @project_id
  AND (project_views.owner_id = @user_id OR project_views.shared)
ORDER BY project_views.name;

-- name: ListViewsAcrossProjects :many
-- Views across projects a person may use: their own, and those shared.
SELECT project_views.*, users.username AS owner_username
FROM project_views
JOIN users ON users.id = project_views.owner_id
WHERE project_views.project_id IS NULL
  AND (project_views.owner_id = @user_id OR project_views.shared)
ORDER BY project_views.name;
