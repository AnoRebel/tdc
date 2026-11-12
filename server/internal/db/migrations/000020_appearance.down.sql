ALTER TABLE users
  DROP CONSTRAINT IF EXISTS users_appearance_palette_known,
  DROP CONSTRAINT IF EXISTS users_appearance_mode_known;

ALTER TABLE users
  DROP COLUMN IF EXISTS appearance_palette,
  DROP COLUMN IF EXISTS appearance_mode,
  DROP COLUMN IF EXISTS appearance_accent;
