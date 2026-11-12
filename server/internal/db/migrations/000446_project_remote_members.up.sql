/*
Whether a project may be shared with other servers at all.

Off by default, as a room is: a project nobody opened is never sent anywhere,
and a federated request about it is answered as though it did not exist. Its
own column rather than the project room's federated flag, because that flag
opens the room to the room federation endpoints, and a project's stream must
reach other servers only by the project's own rules.
*/
ALTER TABLE projects ADD COLUMN federated BOOLEAN NOT NULL DEFAULT FALSE;

/*
People in a project who are on other servers.

project_members requires a local account, which somebody elsewhere has none of
and should not, for the reasons remote_members gives for rooms. So they are kept
by address, the way the project's stream names them, with a role this server —
the project's home — alone decides. Their permissions in the project come from
that role and nothing else: no grant on their own server reaches here.
*/
CREATE TABLE project_remote_members (
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

    -- user@server, as events carry it.
    address     TEXT        NOT NULL,

    -- The server half of the address, so everybody on one server can be
    -- found, or removed, by one statement.
    home_server TEXT        NOT NULL,

    role        TEXT        NOT NULL DEFAULT 'member',
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    added_by    UUID        REFERENCES users(id) ON DELETE SET NULL,

    PRIMARY KEY (project_id, address),
    CONSTRAINT project_remote_members_role_valid CHECK (role IN ('admin', 'member'))
);

CREATE INDEX project_remote_members_server_idx ON project_remote_members (home_server);
