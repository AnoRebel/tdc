## ADDED Requirements

### Requirement: Projects can be shared between servers

A project, its issues and their discussions SHALL be shareable with other servers under the same home-server model as rooms: the project's home server is authoritative for its membership, permissions and the order of its changes, and other servers hold replicas they MUST NOT write to directly.

A project MUST NOT be shared until somebody permitted to administer it opens it to federation.

#### Scenario: A project is shared only once opened

- **WHEN** a project has not been opened to federation
- **THEN** no other server receives anything about it, and federated requests about it are refused as for a project that does not exist

#### Scenario: A replica matches its stream

- **WHEN** a replica rebuilds a project from the project's event stream
- **THEN** its issues, states, labels, priorities, cycles, milestones, assignments and relations are identical to those the home server holds

### Requirement: Visibility decides which servers receive a project

A public project opened to federation MAY be held by any trusted peer. A private project MUST be delivered only to servers hosting at least one of its members, and a server hosting none MUST NOT learn that the project exists.

#### Scenario: A private project reaches only its members' servers

- **WHEN** a private project has members on one remote server and not another
- **THEN** the first receives the project and the second receives nothing about it

#### Scenario: Losing the last member ends a replica

- **WHEN** the last member on a remote server is removed from a private project
- **THEN** that server stops receiving the project and discards its replica

### Requirement: Changes from replicas are decided by the home server

A change made by a person on a replica SHALL be sent to the home server as a signed request in that person's name, and the home server MUST authorise it against its own record of the project's members and permissions before recording it. A replica MUST NOT allocate a position in a project's stream.

#### Scenario: A remote member files an issue

- **WHEN** a member on a remote server files an issue in a shared project
- **THEN** the issue is recorded by the home server and appears on every server holding the project, in the home server's order

#### Scenario: A remote member without permission is refused

- **WHEN** a remote member attempts a change their project role does not allow
- **THEN** the home server refuses it, naming the permission, and nothing is recorded

### Requirement: What never crosses with a project

Saved views, personal settings and drafts SHALL remain on the server where they were made, and a directory engagement linked to a project MUST NOT be shared with the project.

#### Scenario: An engagement's terms stay home

- **WHEN** a project linked to a directory engagement is shared with another server
- **THEN** nothing about the engagement reaches that server
