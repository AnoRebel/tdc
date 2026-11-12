DROP TABLE IF EXISTS message_search;
-- The extension is left in place: another table may be using it, and dropping
-- a shared extension on a rollback would break them.
