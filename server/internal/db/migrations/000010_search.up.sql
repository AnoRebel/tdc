-- Full-text search over messages.

-- Trigram matching, for autocomplete and typo tolerance.
--
-- websearch_to_tsquery cannot do prefix matching — "mkut" will not find
-- "mkutano" — so a search-as-you-type box needs a second path. Trigrams give
-- that and tolerate a misspelling at the same time.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Search text lives in its own table rather than as a column on events.
--
-- Following Synapse's arrangement, for a reason that matters operationally:
-- the index can be dropped and rebuilt without touching the messages
-- themselves. A rebuild on a busy server is then a background job over a
-- separate table rather than a rewrite of the event log, and a corrupted index
-- costs the index rather than the conversation.
CREATE TABLE message_search (
    event_id   TEXT        PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    -- Denormalised so that a search can filter by room and order by recency
    -- without joining back to events. The join would be correct and slower,
    -- and search is on the path of somebody waiting.
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id  UUID        REFERENCES users(id) ON DELETE SET NULL,
    sender_address TEXT    NOT NULL,
    body       TEXT        NOT NULL,
    sent_at    BIGINT      NOT NULL,

    -- Generated rather than maintained by a trigger: the column cannot drift
    -- from the text it indexes, because Postgres computes it on every write.
    --
    -- 'english' is the default configuration. It is wrong for Swahili, which
    -- has no stemmer of its own — but 'simple' would drop no stop words and
    -- stem nothing at all, which is worse for the English that dominates
    -- technical conversation here. An operator whose community writes mostly
    -- in another language can change this; the honest position is that
    -- Postgres has no Swahili configuration and pretending otherwise would be
    -- a silent downgrade.
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('english', body)) STORED
);

-- The index full-text search runs on.
CREATE INDEX message_search_vector_idx ON message_search USING GIN (search_vector);

-- Trigrams over the raw text, for prefix and fuzzy matching.
--
-- GIN rather than GiST: this table is written once per message and read far
-- more often, which is the shape GIN is faster for.
CREATE INDEX message_search_trgm_idx ON message_search USING GIN (body gin_trgm_ops);

-- Results are ordered by recency, not by rank.
--
-- Chat messages are short, so nearly every term appears exactly once and
-- ts_rank degenerates into noise. What people actually want from searching a
-- conversation is the most recent match, which this index serves directly.
CREATE INDEX message_search_room_recency_idx ON message_search (room_id, sent_at DESC);
