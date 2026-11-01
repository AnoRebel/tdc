/*
A picture somebody chose for themselves.

Where it is stored, and as what, are kept on the account: the key changes
with every new picture, which is what lets its address be cached for good,
and it is the only record of where the file is. A picture replaced, removed,
or whose account is erased leaves its file queued for deletion, by trigger,
so none of those paths can forget it.
*/
ALTER TABLE users
  ADD COLUMN avatar_key  TEXT NOT NULL DEFAULT '',
  ADD COLUMN avatar_type TEXT NOT NULL DEFAULT '';

CREATE FUNCTION queue_old_avatar() RETURNS trigger AS $$
BEGIN
    IF OLD.avatar_key <> '' AND (TG_OP = 'DELETE' OR NEW.avatar_key IS DISTINCT FROM OLD.avatar_key) THEN
        INSERT INTO storage_deletions (key) VALUES (OLD.avatar_key) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NULL;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_queue_old_avatar AFTER UPDATE OF avatar_key OR DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION queue_old_avatar();
