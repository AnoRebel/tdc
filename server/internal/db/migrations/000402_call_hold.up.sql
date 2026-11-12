-- Somebody in more than one call talks in one and holds the others; those in a
-- held call see that they are holding rather than wondering why they hear
-- nothing.
ALTER TABLE call_participants ADD COLUMN held BOOLEAN NOT NULL DEFAULT FALSE;
