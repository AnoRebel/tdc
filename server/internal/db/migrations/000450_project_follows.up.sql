/*
A server following a public project, and people elsewhere asking to join one.

A public project opened to federation may be held by any server this one
trusts, not only those hosting a member: a server that follows it holds a
replica its people may read, and keeps it for as long as the project stays
public and open. followed records that it asked, beside the members that
entitle a server otherwise.
*/
ALTER TABLE project_replicas ADD COLUMN followed BOOLEAN NOT NULL DEFAULT FALSE;

/*
Somebody on another server asking to be a member of a project held there.

Asked through their own server, which speaks only for its own people, and
decided here by the project's administrators: accepting makes them a member,
declining forgets the request. Nothing is granted by asking.
*/
CREATE TABLE project_join_requests (
    project_id   UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    address      TEXT        NOT NULL,
    home_server  TEXT        NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, address)
);
