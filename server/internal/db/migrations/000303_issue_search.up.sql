/*
Full-text search over issues, beside the issues rather than in them.

The same arrangement as message_search, for the same reason: the index can be
dropped and rebuilt without touching the rows it indexes. Reindexing an issue
after its title changes writes here and only here, so rebuilding a corrupted
index costs the index, never the issues.

Authorisation is not stored. A search joins projects and project_members at
the moment it runs, so an issue in a project that has just gone private stops
appearing to outsiders immediately, with nothing here to update.
*/
CREATE TABLE issue_search (
    issue_id    UUID        PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
    -- Denormalised so a search in one project filters without a join.
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL,

    /*
      Generated, so it cannot drift from the text it indexes.

      The title weighted above the description: somebody searching for
      "login" wants the issue called that before the one that mentions it in
      passing. 'english' for the reason message_search gives — Postgres has no
      Swahili configuration, and pretending otherwise would be a silent
      downgrade.
    */
    search_vector tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', title), 'A') ||
        setweight(to_tsvector('english', description), 'B')
    ) STORED
);

CREATE INDEX issue_search_vector_idx ON issue_search USING GIN (search_vector);

-- Trigrams over titles, for search-as-you-type, which a tsquery cannot do.
CREATE INDEX issue_search_title_trgm_idx ON issue_search USING GIN (title gin_trgm_ops);

CREATE INDEX issue_search_project_idx ON issue_search (project_id);

-- Every issue that exists already, so upgrading does not leave the old ones
-- unfindable.
INSERT INTO issue_search (issue_id, project_id, title, description)
SELECT id, project_id, title, description FROM issues WHERE deleted_at IS NULL;
