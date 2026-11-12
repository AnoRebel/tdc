## Purpose

Defines how independently hosted TDC servers identify each other, authenticate their exchanges, and share rooms and messages, so that no single organisation is a chokepoint for the community — while deliberately avoiding the replicated-state problems that make general-purpose federation protocols so costly to implement.

## Requirements

### Requirement: Federation is optional

Federation SHALL be an optional capability that an operator can leave disabled, and a server with federation disabled MUST function fully as a standalone deployment.

A server MUST NOT accept federated traffic unless the operator has explicitly enabled it.

#### Scenario: Standalone server is unaffected

- **WHEN** an operator runs a server with federation disabled
- **THEN** all local capabilities function normally
- **AND** requests from other servers are refused

#### Scenario: Enabling is explicit

- **WHEN** a server is installed with default configuration
- **THEN** federation is disabled until the operator enables it

### Requirement: Server identity and discovery

Each participating server SHALL be identified by a domain name, and MUST publish, at a well-known location under that domain, the information another server needs to reach and verify it, including its public signing keys and the protocol versions it supports.

A server MUST verify that a peer's published identity matches the domain it claims before exchanging any content.

#### Scenario: Peer is discoverable

- **WHEN** a server needs to contact a peer for the first time
- **THEN** it retrieves the peer's published federation information from the well-known location under that peer's domain

#### Scenario: Mismatched identity is rejected

- **WHEN** a peer presents an identity that does not match the domain it claims
- **THEN** the connection is refused and no content is exchanged

#### Scenario: Unsupported version is refused cleanly

- **WHEN** two servers share no supported protocol version
- **THEN** they refuse to federate with a clear, logged reason rather than failing obscurely

### Requirement: Request authentication

Every federated request SHALL be signed by the originating server, and the receiving server MUST verify the signature against the originator's published keys before acting on it.

Signatures MUST cover the request's method, path, body, destination, and a timestamp, so that a captured request cannot be replayed against a different server or after it has expired.

#### Scenario: Unsigned request is refused

- **WHEN** a federated request arrives without a valid signature
- **THEN** it is refused and no state changes

#### Scenario: Replay is refused

- **WHEN** a previously valid federated request is replayed after its timestamp window has passed
- **THEN** it is refused

#### Scenario: Request cannot be redirected to another server

- **WHEN** a request signed for one destination server is presented to a different server
- **THEN** it is refused

### Requirement: Rooms have a single home server

Every room SHALL have exactly one home server, which is authoritative for that room's membership, permissions, ordering, and moderation.

Other servers participating in a room MUST treat the home server's decisions as final and MUST NOT attempt to resolve conflicting state independently.

#### Scenario: Home server decides membership

- **WHEN** a user from a remote server joins a room
- **THEN** the room's home server decides whether to admit them
- **AND** other participating servers reflect that decision

#### Scenario: Ordering comes from the home server

- **WHEN** participants on several servers post to a room concurrently
- **THEN** the home server establishes the order and all participating servers present that same order

#### Scenario: No independent conflict resolution

- **WHEN** a participating server receives events implying a state it disagrees with
- **THEN** it defers to the home server rather than computing its own resolution

### Requirement: Cross-server membership

A user SHALL be able to participate in a room hosted by a server other than their own, with their home server acting on their behalf.

The room's home server MUST enforce its own permissions on remote users exactly as it does on local ones.

#### Scenario: Remote user participates

- **WHEN** a user joins a room on a remote server and posts a message
- **THEN** the message appears to all participants in that room
- **AND** it is attributed to that user at their home server

#### Scenario: Remote user is subject to room permissions

- **WHEN** a remote user attempts an action their permissions in that room do not allow
- **THEN** the room's home server refuses it

#### Scenario: Home server outage affects only its own users

- **WHEN** a participating server becomes unreachable
- **THEN** users on other servers continue to use the room normally

### Requirement: Message propagation

The home server SHALL deliver a room's events to every participating server, and MUST retry delivery to a server that is temporarily unreachable.

A server that has been unreachable MUST be able to obtain the events it missed on return, and repeated delivery MUST NOT produce duplicates.

#### Scenario: Events reach participating servers

- **WHEN** an event is accepted into a room
- **THEN** it is delivered to every server with participants in that room

#### Scenario: Unreachable peer is retried and catches up

- **WHEN** a participating server is unreachable and later returns
- **THEN** delivery is retried with backoff
- **AND** the returning server obtains the events it missed

#### Scenario: Redelivery does not duplicate

- **WHEN** an event is delivered more than once
- **THEN** the receiving server stores and displays it exactly once

#### Scenario: Permanently unreachable peer is abandoned

- **WHEN** a peer remains unreachable beyond the configured limit
- **THEN** the server stops retrying, records the fact, and informs the operator

### Requirement: Files travel with messages

A message's files SHALL reach every server participating in its room. Each file MUST be described in the message's signed content by its name, type, size and SHA-256 digest, so that a server obtaining the bytes from anywhere can check them against what the author's server signed.

A server SHALL obtain a file's bytes only when one of its members asks for them, from the room's home server — or, on the home server, from the server that originated the message — with a signed request. The serving server MUST answer only a peer participating in the room, and only for a file a message in that room carries. The receiving server MUST check the length and digest, MUST determine the type from the bytes rather than the description, and MUST apply its own limits, including a largest file it will copy that its operator sets. A copy MUST be made once per server, not once per reader.

Deleting the message on its home server MUST remove the copies on servers that receive the deletion.

#### Scenario: A file sent on one server is read on another

- **WHEN** a member on one server sends a file into a room shared with another
- **THEN** a member on the other server sees it and downloads exactly the bytes that were sent

#### Scenario: Altered bytes are refused

- **WHEN** the bytes a server receives do not match the digest in the signed message
- **THEN** nothing is stored and the member is told the file could not be fetched

#### Scenario: A server with no part in the room is refused

- **WHEN** a peer that does not participate in a room asks for one of its files
- **THEN** it is refused, as though the file did not exist

#### Scenario: A file over the copy limit says so

- **WHEN** a file is larger than the receiving server copies
- **THEN** its name and size are still shown, with an explanation in place of the file

### Requirement: Content verification

A server SHALL verify the signature of every event it receives from a peer against the signing keys of the server that originated it, and MUST reject an event that fails verification.

A server MUST NOT accept an event that claims to originate from a user whose home server did not sign it.

#### Scenario: Forged event is rejected

- **WHEN** a server receives an event whose signature does not verify against the claimed originator's keys
- **THEN** the event is rejected and not stored

#### Scenario: Impersonation is rejected

- **WHEN** a server presents an event attributed to a user of a different server, not signed by that server
- **THEN** the event is rejected

### Requirement: Trust and blocking

Operators SHALL be able to control which servers they federate with, by allowing only listed servers or by blocking specific ones.

Blocking a server MUST stop both incoming and outgoing exchange with it, and MUST be effective immediately.

#### Scenario: Blocklist is enforced both ways

- **WHEN** an operator blocks a server
- **THEN** requests from that server are refused
- **AND** no further content is sent to it

#### Scenario: Allowlist excludes everything else

- **WHEN** an operator configures an allowlist
- **THEN** federation proceeds only with listed servers

#### Scenario: Blocking is auditable

- **WHEN** an operator changes federation trust settings
- **THEN** the change is recorded in the audit log

### Requirement: Connected servers are visible and have a visibility of their own

A person SHALL be able to see the servers their own server is connected to, and to browse the rooms each connected server publishes, without leaving the application.

Each connected server SHALL carry a visibility that its own operator sets, in the same way a room does:

- **public** — any connected server may discover it and browse its published rooms;
- **limited** — only servers the operator has listed may discover it, and it is not returned to others;
- **private** — it is not discoverable at all, and is reachable only by a server that already holds an explicit connection to it.

A server's visibility MUST be enforced by the server itself, not by the client presenting it: a server MUST NOT return a peer, or a peer's rooms, to a requester the peer's visibility excludes.

A room's own visibility MUST still apply after its server has been discovered, so that a private room on a public server remains undiscoverable.

#### Scenario: Public server is browsable

- **WHEN** a person browses connected servers
- **THEN** servers whose visibility is public are listed
- **AND** the rooms those servers publish can be browsed

#### Scenario: Limited server is hidden from servers not listed

- **WHEN** a server whose visibility is limited is asked for its rooms by a server it has not listed
- **THEN** it is not returned, and no rooms are disclosed

#### Scenario: Private server is not discoverable

- **WHEN** a person browses connected servers
- **THEN** servers whose visibility is private are not listed to anyone who does not already hold a connection to them

#### Scenario: Room visibility survives server discovery

- **WHEN** a person browses a public connected server
- **THEN** only rooms that server publishes are listed
- **AND** its private rooms remain undiscoverable

#### Scenario: Visibility change is auditable

- **WHEN** an operator changes their server's visibility
- **THEN** the change is recorded in the audit log

### Requirement: Local policy is preserved

A server SHALL apply its own moderation and content policy to federated content shown to its users, and MUST be able to remove content locally without the cooperation of the originating server.

A server MUST NOT be required to store content its policy forbids.

#### Scenario: Local moderation applies to remote content

- **WHEN** federated content violates the receiving server's policy
- **THEN** that server may remove it for its own users regardless of the originating server

#### Scenario: Local removal does not require cooperation

- **WHEN** a server removes federated content locally
- **THEN** the removal takes effect for its users without any action by the originating server

### Requirement: Privacy across servers

A server SHALL send to a peer only the content that peer's participants are entitled to see, and MUST NOT disclose local users, private channels, or membership of rooms the peer does not participate in.

#### Scenario: Unrelated content is not disclosed

- **WHEN** a server exchanges events for a shared room
- **THEN** no information about unrelated rooms, users, or private channels is included

#### Scenario: Directory data is not implicitly shared

- **WHEN** two servers federate
- **THEN** neither gains access to the other's user directory unless explicitly configured to share it

### Requirement: Protocol is documented and versioned

The federation protocol SHALL be specified in versioned documentation sufficient for an independent implementation, published alongside the software under the same licence.

A change to the protocol MUST increment its version, and servers MUST negotiate a mutually supported version.

#### Scenario: Independent implementation is possible

- **WHEN** a developer reads the published protocol documentation
- **THEN** it describes discovery, signing, event format, and delivery in enough detail to implement a compatible server without reading the reference source

#### Scenario: Versions are negotiated

- **WHEN** servers supporting different protocol versions connect
- **THEN** they negotiate the highest version both support, or refuse with a clear reason

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

#### Scenario: A public project is followed and read, not changed

- **WHEN** a trusted server follows a public project opened to federation, with nobody there a member
- **THEN** it holds a replica its people may read, and their changes are refused until the home server makes them members

#### Scenario: Asking to join is decided by the home server

- **WHEN** somebody reading a followed project asks to join it
- **THEN** the home server records the request for the project's administrators, and they become a member only when an administrator lets them in

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
