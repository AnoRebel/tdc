## Purpose

Defines how files are attached to messages — upload, storage, retrieval, preview generation, and link unfurling — together with the safety rules that prevent attachments and previews from becoming a path to server compromise or data leakage.

## Requirements

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

#### Scenario: A message may be only its files

- **WHEN** a person sends one or more files with no text — in a channel, a thread, a direct or group conversation, a scheduled message, or a room another server holds
- **THEN** the message is posted with its files
- **AND** a message with neither text nor a file is still refused

### Requirement: Large files arrive reliably

Uploads SHALL be resumable: a client MUST be able to send a file in pieces, learn how much the server holds, and continue from there after an interruption rather than starting again.

Every limit that applies to a file MUST be checked against its declared size before the first piece is accepted, and checked again against what actually arrived. Pieces MUST be accepted only in order, only from the person who began the upload, and MUST NOT be retrievable as attachments. A piece MUST be small enough to pass a reverse proxy with a modest body limit, and the operator MUST be able to set its size.

#### Scenario: An interrupted upload resumes

- **WHEN** the connection drops part way through an upload
- **THEN** the client asks the server how much it holds and sends only the rest
- **AND** the stored file is identical to the one picked

#### Scenario: A limit refuses before the transfer

- **WHEN** somebody begins an upload larger than the file limit, their quota, or the space the server has left
- **THEN** the upload is refused before any piece is sent, naming the limit

#### Scenario: Somebody else's upload is out of reach

- **WHEN** a person names an upload somebody else began
- **THEN** it is reported as not existing, and nothing they send is added to it

#### Scenario: Pieces are not files

- **WHEN** anybody asks for a piece of an upload as though it were an attachment
- **THEN** nothing is served

### Requirement: Limits and quotas are the operator's to change

The largest file, the largest message, what one person's files may occupy, what the whole server's files may occupy, and the size of upload pieces SHALL be settings an operator changes while the server runs.

The server-wide and per-person limits MUST count uploads in progress at their declared size, so that many uploads at once cannot together exceed them. A person MUST be able to see their quota and how much of it they use before they pick a file.

#### Scenario: A change applies without a restart

- **WHEN** an operator lowers the largest file allowed
- **THEN** the next upload over the new limit is refused

#### Scenario: Many uploads at once cannot overrun the limit

- **WHEN** several uploads begin together whose declared sizes exceed the space left
- **THEN** those that would exceed it are refused when they begin

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

### Requirement: Direct transfers

Where a person wants to transfer a file the server will not store — because it exceeds the limits, or because they would rather it did not rest on a server at all — the client MAY offer a direct transfer between the two devices. Such a transfer MUST be offered as an alternative and never as the only route, MUST require the recipient to accept it, and MUST NOT be presented as more private than it is: the participants' addresses are visible to each other and to whatever relays the connection.

The negotiation SHALL travel over the server's existing authorised live connection, using the same connection servers as calls, and MUST be authorised on every message: only between the two people of an offer the recipient has accepted, both still members of the conversation. The file's bytes MUST NOT pass through or be stored by the server, except through a relay the operator runs and has allowed for this.

When the recipient is offline, declines, does not answer, or the connection cannot be made or fails part way, the sender MUST be offered an ordinary upload of the same file instead.

An operator MUST be able to turn direct transfers off, and to withhold the relay from them while keeping it for calls.

#### Scenario: A direct transfer is accepted before it begins

- **WHEN** a direct transfer is offered
- **THEN** nothing is sent until the recipient accepts

#### Scenario: Only the two people negotiate

- **WHEN** anybody other than the sender and the recipient of an accepted offer sends a negotiation message for it, or either sends one before it is accepted
- **THEN** it is refused and nothing is relayed

#### Scenario: A failed transfer falls back

- **WHEN** a direct transfer cannot connect or fails part way
- **THEN** the sender is offered an ordinary upload of the same file

#### Scenario: The recipient is warned honestly

- **WHEN** an offer is shown to its recipient
- **THEN** it says who is sending what and how large it is
- **AND** that accepting lets each device see the other's network address, and any relay see both

### Requirement: Deletion

Deleting a message SHALL make its attachments unretrievable, and storage occupied by attachments with no remaining references MUST be reclaimable.

#### Scenario: Deleted message's attachment is unreachable

- **WHEN** a message with an attachment is deleted
- **THEN** subsequent requests for that attachment are denied

#### Scenario: Orphaned storage is reclaimable

- **WHEN** a file was uploaded but never sent in a message, or an upload was begun and abandoned, for longer than the operator's chosen period
- **THEN** its storage is released without anybody having to run anything
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
