-- Store each event's nonce.
--
-- The nonce is part of the signed canonical form, so an event cannot be
-- re-verified without it. It also makes identity unique by construction:
-- without it, two events with the same sender, room, type, content and
-- millisecond timestamp derive the same identifier, which a person
-- double-clicking send produces in ordinary use.
ALTER TABLE events ADD COLUMN nonce TEXT NOT NULL DEFAULT '';
