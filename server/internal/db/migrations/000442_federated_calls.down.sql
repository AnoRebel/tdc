DROP TABLE remote_calls;
DROP TABLE call_remote_invites;
ALTER TABLE call_participants DROP COLUMN remote_seen_at;
ALTER TABLE users DROP COLUMN remote_server, DROP COLUMN remote_address;
