DELETE FROM role_permissions
WHERE permission IN ('call.start', 'call.join') AND scope_id IS NULL
  AND role_id IN (SELECT id FROM roles WHERE name = 'Member' AND built_in);
DROP TABLE call_participants;
DROP TABLE calls;
