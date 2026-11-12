DELETE FROM server_config WHERE key IN ('calls.mesh_limit', 'calls.sfu');
ALTER TABLE calls DROP COLUMN forwarding_node, DROP COLUMN mode;
