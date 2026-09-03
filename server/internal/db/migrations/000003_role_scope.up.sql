-- Correct the scope column on role permissions.
--
-- scope_id must be nullable, because NULL is how a server-scope grant is
-- expressed. It was part of the primary key, which made it NOT NULL and also
-- broke conflict resolution: in a unique index NULL never equals NULL, so two
-- server-scope grants of the same permission would both insert rather than one
-- replacing the other.
--
-- The fix is a surrogate key plus a unique index over the COALESCEd scope, so
-- that server-scope rows collide with each other as intended.

ALTER TABLE role_permissions DROP CONSTRAINT role_permissions_pkey;

ALTER TABLE role_permissions
    ALTER COLUMN scope_id DROP NOT NULL;

-- The all-zero UUID stands in for "no scope" in the index only. It is never
-- stored in scope_id itself, which remains NULL for server scope.
CREATE UNIQUE INDEX role_permissions_unique_idx
    ON role_permissions (role_id, permission, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid));

CREATE INDEX role_permissions_role_id_idx ON role_permissions (role_id);
