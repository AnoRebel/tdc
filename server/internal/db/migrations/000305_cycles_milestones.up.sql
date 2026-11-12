/*
Cycles and milestones: time-boxing, and grouping across it.

Both are optional. A project that never makes a cycle or a milestone has none
of these rows, and nothing about its issues or its board reads them — which is
the whole of "a project that does not use cycles works unchanged".

Neither stores how far along it is. Progress is counted when it is asked for,
from the issues' own states, so there is no second number to drift from the
first.
*/

/*
Which calendar a project's dates are on.

A cycle "ends on the 18th" in the team's calendar, and the 18th ends at a
different instant in Dar es Salaam than in Honolulu. Dates are kept as dates,
the way people chose them, and the instants they begin and end at are worked
out in this zone — an IANA name, checked by the server against its own copy of
the zone database. UTC until somebody says otherwise.
*/
ALTER TABLE projects ADD COLUMN time_zone TEXT NOT NULL DEFAULT 'UTC';

/*
What happens to unfinished issues when a cycle ends.

'carry-forward': they move to the next cycle, or to no cycle if there is none.
'remain': they stay in the cycle that ended, which then shows as ended with
work still in it.

Either way nothing is deleted and no issue's state changes: ending a cycle only
ever changes which cycle an issue is in, and records that it did.
*/
ALTER TABLE projects ADD COLUMN cycle_policy TEXT NOT NULL DEFAULT 'carry-forward'
    CONSTRAINT projects_cycle_policy_valid CHECK (cycle_policy IN ('carry-forward', 'remain'));

CREATE TABLE cycles (
    id          UUID        PRIMARY KEY,
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,

    /*
      The days it covers, both included, as the team chose them: a date, not
      an instant, because "the cycle ends on the 18th" is a statement about a
      calendar and not about a moment in UTC.
    */
    starts_on   DATE        NOT NULL,
    ends_on     DATE        NOT NULL,

    /*
      The same days as instants in the project's zone: from the first moment
      of starts_on to the first moment after ends_on. Derived, and worked out
      again whenever the dates or the project's zone change; kept so the
      server can ask "which cycles have ended" without a calendar.
    */
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ NOT NULL,

    -- When the end was dealt with, and under which policy. Null until then.
    closed_at   TIMESTAMPTZ,
    closed_policy TEXT,

    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT cycles_name_not_empty CHECK (length(trim(name)) > 0),
    CONSTRAINT cycles_dates_in_order CHECK (ends_on >= starts_on),
    CONSTRAINT cycles_instants_in_order CHECK (ends_at > starts_at),
    CONSTRAINT cycles_closed_policy_valid
        CHECK (closed_policy IS NULL OR closed_policy IN ('carry-forward', 'remain'))
);

CREATE INDEX cycles_project_idx ON cycles (project_id, starts_at);
-- What the end-of-cycle job looks for: ended, and not yet dealt with.
CREATE INDEX cycles_unclosed_idx ON cycles (ends_at) WHERE closed_at IS NULL;

-- Which cycle an issue is in: at most one, so the issue is the key.
CREATE TABLE issue_cycle (
    issue_id    UUID        PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
    cycle_id    UUID        NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX issue_cycle_cycle_idx ON issue_cycle (cycle_id);

CREATE TABLE milestones (
    id          UUID        PRIMARY KEY,
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',

    /*
      The day it is meant to be reached by, in the project's calendar, and the
      instant that day is over. Advisory: passing it marks the milestone
      overdue and changes nothing else.
    */
    target_date DATE,
    due_at      TIMESTAMPTZ,

    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT milestones_name_not_empty CHECK (length(trim(name)) > 0),
    CONSTRAINT milestones_due_with_date CHECK ((target_date IS NULL) = (due_at IS NULL))
);

CREATE INDEX milestones_project_idx ON milestones (project_id);

-- Which milestone an issue counts towards: at most one, across any cycles.
CREATE TABLE issue_milestone (
    issue_id     UUID        PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
    milestone_id UUID        NOT NULL REFERENCES milestones(id) ON DELETE CASCADE,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX issue_milestone_milestone_idx ON issue_milestone (milestone_id);

-- Planning is a member's business: putting issues into cycles and milestones,
-- and making them. Granted by membership, so no role needs changing here.
