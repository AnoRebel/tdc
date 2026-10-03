## Purpose

Defines the signed, append-only event model that records every message and state change in a TDC server, providing the single ordered history from which timelines, audit records, integrations, and future cross-server federation are all derived.

## Requirements

### Requirement: Events are the system of record

Every user-visible change of state — a message sent, edited, or deleted; a member joining or leaving; a role or permission changing; a channel created or archived; a server setting altered — SHALL be recorded as an event in an append-only log. Derived views such as timelines, membership lists, and unread counts MUST be reconstructible from the event log.

#### Scenario: State change produces an event

- **WHEN** any user-visible state change occurs
- **THEN** a corresponding event is appended to the log before the change is visible to clients

#### Scenario: Views are reconstructible

- **WHEN** a derived view is rebuilt from the event log
- **THEN** the rebuilt view matches the view served before the rebuild

### Requirement: Event identity and addressing

Every event SHALL carry a globally unique identifier derived from its content, the identity of its author, the identity of the server that accepted it, the room it belongs to, a creation timestamp, and its type.

Users and rooms MUST be addressed in a form that names their home server, so that an identifier minted on one server remains unambiguous when it is later seen by another.

#### Scenario: Identifiers name their home server

- **WHEN** a user or room is created on a server
- **THEN** its identifier includes that server's name in a form that remains unique across servers

#### Scenario: Event identifier is content-derived

- **WHEN** an event is created
- **THEN** its identifier is derived from its content such that altering any signed field would yield a different identifier

### Requirement: Event signing

Every event SHALL be signed. A server MUST reject an event whose signature is absent, malformed, or does not verify against the claimed signing key.

Signing keys MUST be rotatable, and events signed by a key that was valid when the event was created MUST remain verifiable after that key is retired.

#### Scenario: Unsigned event is rejected

- **WHEN** an event without a valid signature is submitted
- **THEN** the server rejects it and does not append it to the log

#### Scenario: Tampering is detectable

- **WHEN** any signed field of a stored event is altered
- **THEN** signature verification of that event fails

#### Scenario: Rotated key still verifies history

- **WHEN** a signing key is rotated
- **THEN** events signed by the previous key continue to verify
- **AND** newly created events are signed by the new key

### Requirement: Ordering

Events within a room SHALL have a total order that every participant agrees on, and that order MUST be stable once established.

Clients MUST be able to request events before or after a known position and to detect that they have missed events.

#### Scenario: Order is stable

- **WHEN** two clients read the same range of a room's history
- **THEN** they observe the events in the same order

#### Scenario: Gap is detectable

- **WHEN** a client reconnects after missing events
- **THEN** the client can determine that a gap exists and retrieve the missing events

### Requirement: Idempotency

Submitting the same event more than once SHALL NOT produce duplicates. A client MUST be able to safely retry a submission whose outcome it did not observe.

#### Scenario: Retry does not duplicate

- **WHEN** a client submits an event, does not receive a response, and retries the identical submission
- **THEN** exactly one event exists in the log
- **AND** the client receives the identifier of that single event

### Requirement: Redaction and erasure

Deleting content SHALL remove the content while leaving evidence that an event existed and was removed. Redaction MUST NOT break the verifiability or ordering of surrounding events.

The system MUST additionally support erasure of a user's personal data on request, and MUST document precisely what erasure removes and what it necessarily retains.

#### Scenario: Redaction preserves structure

- **WHEN** an event is redacted
- **THEN** its content is no longer retrievable
- **AND** the surrounding events remain verifiable and correctly ordered

#### Scenario: Erasure is bounded and documented

- **WHEN** a user's data is erased on request
- **THEN** their personal content and profile data are removed
- **AND** the operator can state, from documentation, exactly what remains and why

### Requirement: Federation readiness

The event model SHALL be defined so that events created on one server can be validated by another server without shared secrets, using only the originating server's published signing keys and the event's own content.

The model MUST NOT depend on any state that exists only inside a single server's database.

#### Scenario: Foreign event is independently verifiable

- **WHEN** a server is given an event created by another server, together with that server's published signing keys
- **THEN** it can verify the event's authorship and integrity without contacting any third party

#### Scenario: No single-server assumptions

- **WHEN** the event schema is reviewed against the federation protocol specification
- **THEN** no field required for validation depends on data private to the originating server
