-- name: CreateRole :one
INSERT INTO roles (id, name, description, built_in)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetRoleByID :one
SELECT * FROM roles WHERE id = $1;

-- name: GetRoleByName :one
SELECT * FROM roles WHERE name = $1;

-- name: ListRoles :many
SELECT * FROM roles ORDER BY name;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = $1 AND built_in = FALSE;

-- name: SetRolePermission :exec
-- The conflict target matches the unique index, which COALESCEs a NULL scope
-- so that two server-scope grants of one permission collide as intended.
INSERT INTO role_permissions (role_id, permission, scope_id, granted)
VALUES ($1, $2, $3, $4)
ON CONFLICT (role_id, permission, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO UPDATE SET granted = EXCLUDED.granted;

-- name: RemoveRolePermission :exec
DELETE FROM role_permissions
WHERE role_id = $1 AND permission = $2 AND scope_id IS NOT DISTINCT FROM $3;

-- name: ListRolePermissions :many
SELECT * FROM role_permissions WHERE role_id = $1 ORDER BY permission;

-- name: AssignRole :exec
INSERT INTO user_roles (user_id, role_id, granted_by)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, role_id) DO NOTHING;

-- name: RemoveRoleFromUser :exec
DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2;

-- name: ListRolesForUser :many
SELECT roles.* FROM roles
JOIN user_roles ON user_roles.role_id = roles.id
WHERE user_roles.user_id = $1
ORDER BY roles.name;

-- name: ListGrantsForUser :many
-- Every grant a user holds, across all their roles. One query rather than one
-- per role: a request resolves permissions once and consults them many times.
SELECT role_permissions.permission, role_permissions.scope_id, role_permissions.granted
FROM role_permissions
JOIN user_roles ON user_roles.role_id = role_permissions.role_id
WHERE user_roles.user_id = $1;

-- name: CountUsersWithPermission :one
-- How many people hold a permission at server scope without an explicit
-- denial. The administrator floor uses this to refuse a change that would
-- leave nobody able to administer the server.
SELECT COUNT(DISTINCT user_roles.user_id)
FROM user_roles
JOIN role_permissions ON role_permissions.role_id = user_roles.role_id
WHERE role_permissions.permission = $1
  AND role_permissions.scope_id IS NULL
  AND role_permissions.granted = TRUE
  AND user_roles.user_id NOT IN (
      SELECT inner_roles.user_id
      FROM user_roles AS inner_roles
      JOIN role_permissions AS denials ON denials.role_id = inner_roles.role_id
      WHERE denials.permission = $1
        AND denials.scope_id IS NULL
        AND denials.granted = FALSE
  );
