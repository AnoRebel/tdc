ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tour_progress_is_object;
ALTER TABLE users DROP COLUMN IF EXISTS tour_progress;
