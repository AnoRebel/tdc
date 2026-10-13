-- Who is presenting their screen in a call: one person at a time, held here so
-- two people starting at once cannot both take the stage.
ALTER TABLE calls ADD COLUMN presenter UUID REFERENCES users(id) ON DELETE SET NULL;
-- Whether somebody is sharing their screen, as they report it.
ALTER TABLE call_participants ADD COLUMN screen BOOLEAN NOT NULL DEFAULT FALSE;
