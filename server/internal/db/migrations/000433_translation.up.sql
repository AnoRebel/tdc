/*
Inline message translation: what each person chose, the providers they
connected, and translations already made.
*/

/*
One row per person who has chosen anything. An empty provider means the
default — on-device where the browser has it, otherwise the operator's
LibreTranslate instance — and an empty language means the person's interface
language, so an untouched row follows them rather than freezing a guess.
*/
CREATE TABLE translation_preferences (
    user_id         UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    provider        TEXT        NOT NULL DEFAULT '',
    target_language TEXT        NOT NULL DEFAULT '',
    -- Everything on this server, translated without asking.
    auto_translate  BOOLEAN     NOT NULL DEFAULT FALSE,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One conversation translated, or not, whatever the server-wide choice is.
CREATE TABLE translation_room_rules (
    user_id        UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id        UUID    NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    auto_translate BOOLEAN NOT NULL,
    PRIMARY KEY (user_id, room_id)
);

/*
A provider somebody connected with their own credentials.

The key is sealed (AES-256-GCM, under the operator's translation.secret_key,
with the user and provider as additional data) and is never read back to a
client. The endpoint and region are not secret: they say where, not how.
*/
CREATE TABLE translation_connections (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider   TEXT        NOT NULL CHECK (provider IN ('libretranslate', 'google', 'microsoft', 'deepl')),
    sealed_key BYTEA,
    endpoint   TEXT        NOT NULL DEFAULT '',
    region     TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, provider)
);

/*
Translations already made, shared between readers.

Keyed by the hash of the body that was translated, so an edit is a miss rather
than a stale answer. source_event_id is the event that body came from — the
message itself, or its newest edit — so removing either removes this.
*/
CREATE TABLE message_translations (
    message_id      TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    source_event_id TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    target_language TEXT        NOT NULL,
    provider        TEXT        NOT NULL,
    body_hash       BYTEA       NOT NULL,
    translated_body TEXT        NOT NULL,
    source_language TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (message_id, target_language, provider, body_hash)
);

CREATE INDEX message_translations_source_idx ON message_translations (source_event_id);

/*
A removed message's translation goes with it.

A trigger rather than a statement beside each removal, because a message is
removed in several places — by its author, by a moderator, by retention, by
erasure — and a translation that outlived any one of them would be the removed
words still sitting in the database.
*/
CREATE FUNCTION forget_translations_of_redacted() RETURNS trigger AS $$
BEGIN
    DELETE FROM message_translations
    WHERE message_id = NEW.id OR source_event_id = NEW.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER events_forget_translations
    AFTER UPDATE OF redacted_at ON events
    FOR EACH ROW
    WHEN (NEW.redacted_at IS NOT NULL AND OLD.redacted_at IS NULL)
    EXECUTE FUNCTION forget_translations_of_redacted();
