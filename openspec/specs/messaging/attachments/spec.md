## Purpose

Defines how files are attached to messages — upload, storage, retrieval, preview generation, and link unfurling — together with the safety rules that prevent attachments and previews from becoming a path to server compromise or data leakage.

## ADDED Requirements

### Requirement: Attaching files

Users SHALL be able to attach one or more files to a message, subject to operator-configured limits on file size, total message size, and permitted types.

Limits MUST be enforced by the server, and a rejected upload MUST state which limit it exceeded.

#### Scenario: Upload within limits succeeds

- **WHEN** a user attaches a file within the configured limits to a message
- **THEN** the file is stored and the message references it
- **AND** recipients permitted to read the message can retrieve the file

#### Scenario: Oversized upload is refused clearly

- **WHEN** a user attempts to upload a file exceeding the configured size limit
- **THEN** the upload is refused
- **AND** the response states the limit that was exceeded

#### Scenario: Client-side limits are not trusted

- **WHEN** a client bypasses its own validation and submits a file violating a limit
- **THEN** the server refuses it

### Requirement: Storage backend is pluggable

Attachment storage SHALL be accessed through an interface supporting at minimum the local filesystem and any S3-compatible object store, selectable by configuration.

The default configuration MUST work with no additional service beyond the server itself, and no supported backend may require a paid licence or subscription.

#### Scenario: Default needs no extra service

- **WHEN** an operator starts a server without configuring object storage
- **THEN** attachments work using local filesystem storage

#### Scenario: Object storage is configurable

- **WHEN** an operator configures a self-hosted S3-compatible store
- **THEN** attachments are stored there without code changes

#### Scenario: Backend change does not break existing references

- **WHEN** an operator migrates storage backends following the documented procedure
- **THEN** previously uploaded attachments remain retrievable

### Requirement: Access control on retrieval

Retrieving an attachment SHALL require the same authorization as reading the message it belongs to. Knowledge of an attachment's location MUST NOT by itself grant access.

Where the server issues time-limited direct-download links, those links MUST expire and MUST be bound to the authorized user.

#### Scenario: Non-member cannot retrieve

- **WHEN** a user who cannot read the containing message requests its attachment directly
- **THEN** the request is denied

#### Scenario: Guessed location is not sufficient

- **WHEN** an unauthorized party requests an attachment by its storage location
- **THEN** the request is denied

#### Scenario: Download link expires

- **WHEN** a time-limited download link is used after expiry
- **THEN** the request is denied

### Requirement: Content type verification

The server SHALL determine an uploaded file's type from its content rather than trusting a client-supplied name or declared type, and MUST serve attachments with headers that prevent the browser from interpreting them as active content.

#### Scenario: Declared type is not trusted

- **WHEN** a file's actual content does not match its declared type
- **THEN** the server treats it according to its detected type

#### Scenario: Attachments are served inertly

- **WHEN** any attachment is served
- **THEN** it is sent with headers that prevent content-type sniffing and prevent execution in the browsing context of the application

#### Scenario: Disallowed type is refused

- **WHEN** an operator restricts permitted types and a file outside that set is uploaded
- **THEN** the upload is refused

### Requirement: Previews and thumbnails

The server SHALL generate previews for supported file types, and clients MUST be able to display an attachment's nature without downloading it in full.

Preview generation MUST be resource-bounded and MUST NOT allow a malformed file to exhaust server resources or execute code.

#### Scenario: Image preview avoids full download

- **WHEN** a message with an image attachment is displayed
- **THEN** the client displays a reduced-size preview rather than downloading the full image

#### Scenario: Malformed file cannot exhaust resources

- **WHEN** a malformed or maliciously crafted file is uploaded
- **THEN** preview generation fails within its resource and time bounds
- **AND** the server continues serving other requests normally

#### Scenario: Unpreviewable file degrades gracefully

- **WHEN** a file type has no preview support
- **THEN** the client displays its name, size, and type without error

#### Scenario: Previews respect metered connections

- **WHEN** a client indicates a constrained connection
- **THEN** previews are requested at reduced size or deferred until requested by the user

### Requirement: Link unfurling

When a message contains a link, the server MAY fetch it to produce a preview. Unfurling MUST be individually toggleable by the operator and by the user.

Unfurling MUST NOT be usable to reach hosts the server can access but an outside user cannot.

#### Scenario: Internal addresses are not fetched

- **WHEN** a message contains a link resolving to a loopback, link-local, or private network address
- **THEN** the server does not fetch it and produces no preview

#### Scenario: Redirects cannot escape restrictions

- **WHEN** an unfurl request is redirected to an address that would otherwise be refused
- **THEN** the redirect is not followed

#### Scenario: Unfurling is bounded

- **WHEN** the server fetches a link for preview
- **THEN** the request is subject to timeout, response size, and redirect-count limits
- **AND** exceeding any of them aborts the fetch without a preview

#### Scenario: Unfurling can be disabled

- **WHEN** an operator or a user disables link previews
- **THEN** no outbound fetch is made for that scope

#### Scenario: Private content is not leaked by unfurling

- **WHEN** a link is posted in a private channel
- **THEN** the fetch does not disclose the channel, its members, or the message content to the target host beyond the request itself

### Requirement: Original media is retrievable

Where the client reduces a file before uploading it, the person sending it SHALL be able to send the original instead, and MUST be told which they are sending.

A reduced copy is the default because most sharing is of photographs to be looked at, and on metered mobile data the original costs the sender money for detail nobody sees. Sending the original is a deliberate choice, not a hidden one.

An operator MAY set a size above which the original is refused, and that refusal MUST name the limit as any other refusal does.

Where a person wants to transfer a file the server will not store — because it exceeds the limits, or because they would rather it did not rest on a server at all — the client MAY offer a direct transfer between the two devices. Such a transfer MUST be offered as an alternative and never as the only route, MUST require the recipient to accept it, and MUST NOT be presented as more private than it is: the participants' addresses are visible to each other and to whatever relays the connection.

#### Scenario: The reduced copy is the default

- **WHEN** a person attaches a photograph
- **THEN** a reduced copy is sent unless they choose otherwise

#### Scenario: Sending the original is offered

- **WHEN** a person chooses to send the original
- **THEN** the file is uploaded unreduced
- **AND** the interface says which was sent

#### Scenario: An oversized original names the limit

- **WHEN** an original exceeds what the operator permits
- **THEN** the refusal states the limit it exceeded

#### Scenario: A direct transfer is accepted before it begins

- **WHEN** a direct transfer is offered
- **THEN** nothing is sent until the recipient accepts

### Requirement: Deletion

Deleting a message SHALL make its attachments unretrievable, and storage occupied by attachments with no remaining references MUST be reclaimable.

#### Scenario: Deleted message's attachment is unreachable

- **WHEN** a message with an attachment is deleted
- **THEN** subsequent requests for that attachment are denied

#### Scenario: Orphaned storage is reclaimable

- **WHEN** an operator runs the documented cleanup procedure
- **THEN** storage for attachments with no remaining references is released
- **AND** attachments still referenced are untouched

### Requirement: Malware scanning hook

The server SHALL provide an optional, operator-configured hook for scanning uploads before they are made available.

The hook MUST be disabled by default, MUST NOT require a paid service, and its failure MUST fail closed unless the operator has configured otherwise.

#### Scenario: Scanning is optional

- **WHEN** no scanner is configured
- **THEN** uploads proceed without one and the server does not require any external service

#### Scenario: Rejected upload is not delivered

- **WHEN** a configured scanner rejects an uploaded file
- **THEN** the attachment is not made available to recipients
- **AND** the uploader is informed

#### Scenario: Scanner failure fails closed by default

- **WHEN** a configured scanner is unreachable
- **THEN** the upload is refused unless the operator has explicitly configured fail-open
