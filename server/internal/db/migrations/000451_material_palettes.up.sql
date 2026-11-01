/*
Crystal Neuo's six materials, as palettes somebody may choose.

A material is more than a set of colours — it carries its own corners,
borders, shadows and type — but it is chosen the way a palette is and replaces
one, so it is stored in the same column rather than beside it, where the two
could disagree about what somebody sees.
*/
ALTER TABLE users
  DROP CONSTRAINT users_appearance_palette_known,
  ADD CONSTRAINT users_appearance_palette_known
    CHECK (appearance_palette IN (
      '', 'default', 'congrator',
      'refraction', 'tactile', 'signal', 'folio', 'bloom', 'hybrid'
    ));
