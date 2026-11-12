DROP INDEX IF EXISTS role_permissions_role_id_idx;
DROP INDEX IF EXISTS role_permissions_unique_idx;
DELETE FROM role_permissions WHERE scope_id IS NULL;
ALTER TABLE role_permissions ALTER COLUMN scope_id SET NOT NULL;
ALTER TABLE role_permissions ADD PRIMARY KEY (role_id, permission, scope_id);
