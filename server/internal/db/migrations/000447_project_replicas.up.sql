/*
Projects held as replicas of another server's, and the servers this one's are
held by.

A replica's tables are written only from the project's stream, by the server
holding the copy; nothing else here writes them.
*/

/*
A replica's key names its home: WEB@a.example rather than WEB, so a copy can
never take the key of a project started here, and an issue identifier read on
this server — WEB@a.example-12 — says where the issue lives. A project started
here still has a key of letters and digits alone.
*/
ALTER TABLE projects DROP CONSTRAINT projects_key_shape;
ALTER TABLE projects ADD CONSTRAINT projects_key_shape
    CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}(@[a-z0-9.:-]+)?$');

/*
Why a replica stopped following its stream, or empty while it follows.

A replica meeting an event it cannot apply — one a newer home server records —
stops there rather than skipping it, since skipping would quietly disagree with
the home server. This says so to anybody looking at the project.
*/
ALTER TABLE projects ADD COLUMN replica_error TEXT NOT NULL DEFAULT '';

/*
An issue written by somebody on another server.

Only on a replica, where the author has no account: the address is what the
stream names them by. A local author is author_id, as always.
*/
ALTER TABLE issues ADD COLUMN author_address TEXT;

/*
People on other servers assigned to an issue.

issue_assignees needs a local account, as project_members does; this is to it
what project_remote_members is to project_members.
*/
CREATE TABLE issue_remote_assignees (
    issue_id    UUID        NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    address     TEXT        NOT NULL,
    home_server TEXT        NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (issue_id, address)
);

/*
The servers holding a replica of a project this server is home to.

Each is first offered the project — told it exists and who of theirs is in it
— and only once it has accepted are the project's events delivered to it. When
it loses its last member, or the project is closed to federation, it is told
to discard its copy, retried until it confirms. Both are retried with backoff,
since the other server may be away.
*/
CREATE TABLE project_replicas (
    project_id      UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- The server, as a host, the way room peers are named.
    server          TEXT        NOT NULL,
    -- Where to reach it.
    address         TEXT        NOT NULL,
    offered_at      TIMESTAMPTZ,
    discarding      BOOLEAN     NOT NULL DEFAULT FALSE,
    failures        INTEGER     NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ,
    last_error      TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (project_id, server)
);
