DROP TRIGGER IF EXISTS users_queue_old_avatar ON users;
DROP FUNCTION IF EXISTS queue_old_avatar();
ALTER TABLE users DROP COLUMN IF EXISTS avatar_type, DROP COLUMN IF EXISTS avatar_key;
