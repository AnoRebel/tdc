/*
Projects, their states, and the issues filed in them.

A project is two things at once, and both are rows here. It is a container
with a key, a name and a membership, which is the projects table. And it is a
stream of events — every change to every issue in it, signed and ordered — and
that stream is a room, of a kind nobody talks in.

Making the stream a room rather than a second log is the point. The event log
already knows how to sign, number, verify, redact and erase; a project whose
history lived anywhere else would need every one of those again, and would give
"what does tamper-evident mean here" a second answer. So a project's row shares
its identifier with a room of kind 'project', and deleting the room deletes the
project with it.

An issue is the same arrangement once more: its discussion is a room of kind
'issue-discussion' sharing the issue's identifier, where comments are ordinary
messages. The issue itself is not a room. It is a row with an identifier that
never changes, which a room has never needed to promise.
*/

-- Two new kinds of room. Neither is a place anybody finds in a channel list:
-- the listings exclude both, by kind.
ALTER TABLE rooms DROP CONSTRAINT rooms_kind_valid;
ALTER TABLE rooms ADD CONSTRAINT rooms_kind_valid
    CHECK (kind IN ('channel', 'direct', 'disposable', 'project', 'issue-discussion'));

CREATE TABLE projects (
    -- The same identifier as the project's event stream, which is a room.
    id          UUID        PRIMARY KEY REFERENCES rooms(id) ON DELETE CASCADE,

    /*
      What every issue identifier starts with: WEB in WEB-12.

      Never changed once chosen. An identifier people have written in commit
      messages and said aloud in meetings has to keep meaning the same issue,
      and a project whose key could change would break every one of them at
      once. Renaming a project changes its name, not this.
    */
    key         TEXT        NOT NULL,
    -- Folded, so WEB and web cannot both exist and be confused in speech.
    key_folded  TEXT        NOT NULL UNIQUE,

    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',

    -- 'public': anybody on this server may see it and its issues.
    -- 'private': only its members may, and to anybody else it does not exist.
    visibility  TEXT        NOT NULL DEFAULT 'public',

    /*
      The next issue number, allocated under the row's lock.

      The same reasoning as room_sequences: MAX(number) + 1 is a read two
      people filing at once can both see, and they would both be issue 12.
      Incrementing a locked row gives one number each.
    */
    next_issue_number BIGINT NOT NULL DEFAULT 1,

    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT projects_visibility_valid CHECK (visibility IN ('public', 'private')),
    CONSTRAINT projects_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    CONSTRAINT projects_name_not_empty CHECK (length(trim(name)) > 0)
);

/*
Who is in a project.

Shaped like room_members on purpose, and a separate table on purpose. A
project's membership answers "may this person see and work on these issues",
a room's answers "may this person read these messages", and one table serving
both would need a join to discover which question each row answers.

role is what the membership confers inside the project: an 'admin' runs it —
its settings, its states, its members — and a 'member' works in it.
*/
CREATE TABLE project_members (
    project_id UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT        NOT NULL DEFAULT 'member',
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    added_by   UUID        REFERENCES users(id) ON DELETE SET NULL,

    PRIMARY KEY (project_id, user_id),
    CONSTRAINT project_members_role_valid CHECK (role IN ('admin', 'member'))
);

CREATE INDEX project_members_user_idx ON project_members (user_id);

/*
The states an issue moves between, per project.

Rows rather than an enumeration, because the people running a project decide
what its workflow is. is_completion is what makes "done" unambiguous: a
project may call it Done, Shipped or Imefanyika, and progress still knows which
states count.
*/
CREATE TABLE issue_states (
    id            UUID        PRIMARY KEY,
    project_id    UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    name_folded   TEXT        NOT NULL,
    -- Order on a board, left to right. Gaps are fine; only the order matters.
    position      INTEGER     NOT NULL,
    is_completion BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT issue_states_name_not_empty CHECK (length(trim(name)) > 0),
    UNIQUE (project_id, name_folded)
);

CREATE INDEX issue_states_project_idx ON issue_states (project_id, position);

CREATE TABLE issues (
    -- The same identifier as the issue's discussion room.
    id          UUID        PRIMARY KEY REFERENCES rooms(id) ON DELETE CASCADE,
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

    -- 12 in WEB-12. With the project's key, the identifier people use.
    number      BIGINT      NOT NULL,

    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',

    /*
      The state the issue is in.

      Neither cascading nor nulling: a state still in use cannot be deleted out
      from under the issues in it, so no issue can ever name a state that does
      not exist. Removing one means moving its issues first, which the
      application does in the same transaction.

      NO ACTION rather than RESTRICT, because the check then waits for the end
      of the statement. Deleting a whole project removes its states and its
      issues in one cascade, and RESTRICT would refuse the first state it
      reached while that state's issues were still waiting their turn.

      These mutable columns are a cache of the project's event stream, written
      only by the code that appends the event justifying them and in the same
      transaction. The stream is the truth; this is what a board can query
      without replaying it.
    */
    state_id    UUID        NOT NULL REFERENCES issue_states(id) ON DELETE NO ACTION,

    author_id   UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    /*
      When it was deleted, if it was.

      Deletion hides an issue rather than removing it. Its history is events,
      and events are not removed; an issue that vanished while its history
      remained would leave a stream describing something nobody can look up.
    */
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID        REFERENCES users(id) ON DELETE SET NULL,

    CONSTRAINT issues_title_not_empty CHECK (length(trim(title)) > 0),
    UNIQUE (project_id, number)
);

CREATE INDEX issues_project_state_idx ON issues (project_id, state_id) WHERE deleted_at IS NULL;

/*
An issue's history, found in its project's stream.

Every issue event names its issue in content->>'issue', and the history of one
issue is its project's stream filtered to that — an indexed read rather than a
scan of the stream, and no second place history could be written to.
*/
CREATE INDEX events_issue_idx ON events ((content->>'issue'), sequence)
    WHERE type LIKE 'issue.%';

/*
Starting a project is something every member may do, as making a channel is.

For a server that already exists: its role for every member was created before
this permission did, and would otherwise leave projects to administrators
alone. A fresh server gets the same grant when that role is first made. An
operator who has already decided otherwise can withdraw it like any other.
*/
INSERT INTO role_permissions (role_id, permission, scope_id, granted)
SELECT id, 'project.create', NULL, TRUE FROM roles WHERE name = 'Member' AND built_in
ON CONFLICT DO NOTHING;
