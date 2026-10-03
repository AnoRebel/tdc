/*
What makes an issue trackable rather than merely filed: who is on it, what
kind of thing it is, how urgent, and how it relates to the others.

Every table here is, like the issue's own columns, a cache of the project's
event stream — written only by the code that appends the event recording the
change, in the same transaction.
*/

-- Who is working on an issue. More than one person may be.
CREATE TABLE issue_assignees (
    issue_id    UUID        NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_by UUID        REFERENCES users(id) ON DELETE SET NULL,

    PRIMARY KEY (issue_id, user_id)
);

-- "My issues" reads this by person, across every project.
CREATE INDEX issue_assignees_user_idx ON issue_assignees (user_id);

/*
A project's labels.

Per project, because what "needs-design" means is the project's business, and
a server-wide vocabulary would be one somebody has to administer.
*/
CREATE TABLE project_labels (
    id          UUID        PRIMARY KEY,
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    name_folded TEXT        NOT NULL,
    -- A hex colour, #rrggbb, chosen from the palette the client offers.
    colour      TEXT        NOT NULL DEFAULT '#6b7280',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT project_labels_name_not_empty CHECK (length(trim(name)) > 0),
    CONSTRAINT project_labels_colour_hex CHECK (colour ~ '^#[0-9a-f]{6}$'),
    UNIQUE (project_id, name_folded)
);

CREATE TABLE issue_labels (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES project_labels(id) ON DELETE CASCADE,

    PRIMARY KEY (issue_id, label_id)
);

-- Filtering a project's issues by label reads from the label's side.
CREATE INDEX issue_labels_label_idx ON issue_labels (label_id);

/*
A project's priority scale, most urgent first.

Configurable rather than fixed, because some teams want three levels and some
want five, and the words matter less than the order: sorting by priority is
sorting by rank, whatever the ranks are called.
*/
CREATE TABLE project_priorities (
    id          UUID        PRIMARY KEY,
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    name_folded TEXT        NOT NULL,
    -- 1 is the most urgent. Gaps are fine; only the order matters.
    rank        INTEGER     NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT project_priorities_name_not_empty CHECK (length(trim(name)) > 0),
    UNIQUE (project_id, name_folded)
);

/*
An issue's priority, or none.

NO ACTION for the same reason as its state: a priority still in use cannot
disappear out from under the issues that have it, because clearing it silently
would be a change nobody recorded. Removing one clears it from each issue as an
ordinary recorded change first.
*/
ALTER TABLE issues ADD COLUMN priority_id UUID REFERENCES project_priorities(id) ON DELETE NO ACTION;

/*
How issues relate: one is another's parent, or one blocks another.

Stored once, as a directed edge — parent to child, blocker to blocked — with
the inverse implied, so there is exactly one row to agree or disagree with.
Neither relationship may form a cycle, which the application refuses before
writing; the constraint here only stops an issue relating to itself.
*/
CREATE TABLE issue_relations (
    from_issue UUID        NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    to_issue   UUID        NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    -- 'parent': from_issue is the parent of to_issue.
    -- 'blocks': from_issue blocks to_issue.
    kind       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID        REFERENCES users(id) ON DELETE SET NULL,

    PRIMARY KEY (from_issue, to_issue, kind),
    CONSTRAINT issue_relations_kind_valid CHECK (kind IN ('parent', 'blocks')),
    CONSTRAINT issue_relations_not_self CHECK (from_issue <> to_issue)
);

CREATE INDEX issue_relations_to_idx ON issue_relations (to_issue, kind);

-- An issue has at most one parent: a hierarchy, not a web.
CREATE UNIQUE INDEX issue_relations_one_parent_idx ON issue_relations (to_issue) WHERE kind = 'parent';

-- Every existing project gets the scale a new one starts with.
INSERT INTO project_priorities (id, project_id, name, name_folded, rank)
SELECT gen_random_uuid(), projects.id, scale.name, lower(scale.name), scale.rank
FROM projects
CROSS JOIN (VALUES ('Urgent', 1), ('High', 2), ('Medium', 3), ('Low', 4)) AS scale(name, rank);

-- Being assigned is something to be told about.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN (
        'mention', 'direct', 'thread', 'reaction', 'reply', 'join', 'forward', 'pin',
        -- Belongs to outgoing webhooks: one of yours was turned off. Not unknown; keep it.
        'webhook_disabled',
        -- Somebody made you responsible for an issue.
        'assigned'
    ));
