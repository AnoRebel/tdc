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
--
-- baseline marks what comes from the role every account holds, which any
-- other role's entry at the same scope overrides.
SELECT role_permissions.permission, role_permissions.scope_id, role_permissions.granted,
       (roles.built_in AND roles.name = sqlc.arg(baseline_role)::text)::boolean AS baseline
FROM role_permissions
JOIN user_roles ON user_roles.role_id = role_permissions.role_id
JOIN roles ON roles.id = role_permissions.role_id
WHERE user_roles.user_id = sqlc.arg(user_id);

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

-- name: CountUsersWithPermissionOutsideRole :one
-- How many people would still hold a permission at server scope if one role
-- stopped conferring it — withdrawn from the role, denied on it, or the role
-- deleted. Everybody holding that role is left out whatever else they hold: a
-- denial on a role reaches every holder, and a withdrawal from a role two
-- administrators share takes it from both. Counting them in, as the plain
-- count does, let a role every administrator shared be emptied.
SELECT COUNT(DISTINCT user_roles.user_id)
FROM user_roles
JOIN role_permissions ON role_permissions.role_id = user_roles.role_id
WHERE role_permissions.permission = sqlc.arg(permission)
  AND role_permissions.scope_id IS NULL
  AND role_permissions.granted = TRUE
  AND user_roles.user_id NOT IN (
      SELECT holders.user_id FROM user_roles AS holders
      WHERE holders.role_id = sqlc.arg(role_id)
  )
  AND user_roles.user_id NOT IN (
      SELECT inner_roles.user_id
      FROM user_roles AS inner_roles
      JOIN role_permissions AS denials ON denials.role_id = inner_roles.role_id
      WHERE denials.permission = sqlc.arg(permission)
        AND denials.scope_id IS NULL
        AND denials.granted = FALSE
  );

-- name: UpdateRole :one
UPDATE roles SET name = $2, description = $3 WHERE id = $1
RETURNING *;

-- name: ListRolesWithMemberCounts :many
-- Every role and how many people hold it, for the list an administrator
-- chooses from.
SELECT roles.id, roles.name, roles.description, roles.built_in,
       (SELECT COUNT(*) FROM user_roles WHERE user_roles.role_id = roles.id)::bigint AS member_count
FROM roles
ORDER BY roles.built_in DESC, roles.name;

-- name: ListAllRolePermissions :many
-- Every entry on every role, with the name of the channel or project a scoped
-- one applies to. A project is also a room, so the project is looked for
-- first. An entry whose channel has gone keeps an empty name.
SELECT role_permissions.role_id, role_permissions.permission, role_permissions.scope_id,
       role_permissions.granted,
       (projects.id IS NOT NULL)::boolean AS in_project,
       COALESCE(projects.name, rooms.name, '')::text AS scope_name
FROM role_permissions
LEFT JOIN projects ON projects.id = role_permissions.scope_id
LEFT JOIN rooms ON rooms.id = role_permissions.scope_id
ORDER BY role_permissions.permission;

-- name: ListRoleMembers :many
-- Who holds a role, by name. Bounded: on a large server the role every account
-- holds is everybody, and a list of everybody is not what a role's page is for.
SELECT users.id, users.username, users.display_name
FROM user_roles
JOIN users ON users.id = user_roles.user_id
WHERE user_roles.role_id = $1
ORDER BY users.username_folded
LIMIT $2;

-- name: ListGrantSourcesForUser :many
-- Every entry behind what somebody may do, with the role it comes from, for an
-- administrator asking why.
SELECT roles.id AS role_id, roles.name AS role_name,
       role_permissions.permission, role_permissions.scope_id, role_permissions.granted,
       (roles.built_in AND roles.name = sqlc.arg(baseline_role)::text)::boolean AS baseline
FROM role_permissions
JOIN user_roles ON user_roles.role_id = role_permissions.role_id
JOIN roles ON roles.id = role_permissions.role_id
WHERE user_roles.user_id = sqlc.arg(user_id)
ORDER BY roles.name, role_permissions.permission;
