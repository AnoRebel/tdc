UPDATE users SET appearance_palette = ''
  WHERE appearance_palette NOT IN ('', 'default', 'congrator');
ALTER TABLE users
  DROP CONSTRAINT users_appearance_palette_known,
  ADD CONSTRAINT users_appearance_palette_known
    CHECK (appearance_palette IN ('', 'default', 'congrator'));
