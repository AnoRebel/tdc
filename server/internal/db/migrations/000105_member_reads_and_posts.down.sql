DELETE FROM role_permissions
USING roles
WHERE role_permissions.role_id = roles.id
  AND roles.name = 'Member' AND roles.built_in
  AND role_permissions.scope_id IS NULL
  AND role_permissions.permission IN ('channel.read', 'channel.post');
