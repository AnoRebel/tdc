DROP TRIGGER IF EXISTS events_forget_translations ON events;
DROP FUNCTION IF EXISTS forget_translations_of_redacted();
DROP TABLE IF EXISTS message_translations;
DROP TABLE IF EXISTS translation_connections;
DROP TABLE IF EXISTS translation_room_rules;
DROP TABLE IF EXISTS translation_preferences;
