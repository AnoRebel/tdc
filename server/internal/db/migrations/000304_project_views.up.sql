/*
Saved views: a way of looking at issues, kept so it can be used again.

A view stores how to ask, never what was found. Opening one runs its filter
again as whoever opens it, through the same queries a list, a board or a search
runs — so a view shared with somebody shows them what they may see, which can
be less than its owner sees, and never more. Nothing about the owner's access
travels with it.

A view belongs to one project, shown with that project's board or list, or to
no project, when it is a search across every project its reader can see.
*/
CREATE TABLE project_views (
    id          UUID        PRIMARY KEY,
    -- Null for a view across projects.
    project_id  UUID        REFERENCES projects(id) ON DELETE CASCADE,
    owner_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    -- 'list' or 'board'. A view across projects is a list: a board is one
    -- project's states.
    layout      TEXT        NOT NULL DEFAULT 'list',
    /*
      What to ask: { "q", "state", "label", "priority", "assignee", "sort" }.
      Names rather than identifiers, as a search across projects needs, and
      as a view outliving a renamed-and-recreated label wants.
    */
    filter      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Shared with everybody who can see the project, or for a view across
    -- projects, with everybody on the server.
    shared      BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT project_views_name_not_empty CHECK (length(trim(name)) > 0),
    CONSTRAINT project_views_layout_valid CHECK (layout IN ('list', 'board')),
    CONSTRAINT project_views_board_has_project CHECK (layout = 'list' OR project_id IS NOT NULL)
);

CREATE INDEX project_views_project_idx ON project_views (project_id);
CREATE INDEX project_views_owner_idx ON project_views (owner_id);
