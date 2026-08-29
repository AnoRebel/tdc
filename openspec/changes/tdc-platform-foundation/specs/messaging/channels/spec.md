## Purpose

Defines the spaces in which TDC conversation happens — servers, channels, threads, direct messages, and disposable chats — including how they are created, joined, discovered, archived, and how membership governs who can see what.

## ADDED Requirements

### Requirement: Channels

A server SHALL contain channels, each with a name, an optional description or topic, and a visibility setting. Channels MUST be creatable, renameable, and archivable by users holding the corresponding permission.

Archiving a channel MUST preserve its history and stop new messages, and MUST be reversible.

#### Scenario: Channel is created and joinable

- **WHEN** a user with permission creates a public channel
- **THEN** the channel becomes discoverable to members of the server
- **AND** members can join it and read its history according to its visibility

#### Scenario: Archived channel is read-only

- **WHEN** a channel is archived
- **THEN** its history remains readable to those who could read it before
- **AND** attempts to post to it are refused

#### Scenario: Archiving is reversible

- **WHEN** an archived channel is restored by a user with permission
- **THEN** posting resumes and no history was lost

### Requirement: Channel visibility

Channels SHALL support at least public visibility, where any server member may discover and join, and private visibility, where only invited members may discover, read, or join.

A private channel's existence, name, and history MUST NOT be revealed to non-members through any endpoint, search result, notification, or error message.

#### Scenario: Private channel is invisible to non-members

- **WHEN** a user who is not a member queries, searches, or browses
- **THEN** the private channel does not appear in any result

#### Scenario: Private channel content is not leaked by errors

- **WHEN** a non-member requests a private channel directly by identifier
- **THEN** the response does not distinguish "exists but forbidden" from "does not exist"

#### Scenario: Visibility change is auditable

- **WHEN** a channel's visibility changes
- **THEN** the change is recorded in the audit log with the actor and both values

### Requirement: Membership

Users SHALL join and leave channels, and channel membership MUST determine delivery of that channel's messages and notifications.

Removal from a channel MUST take effect immediately for live connections, not only on reconnection.

#### Scenario: Joining begins delivery

- **WHEN** a user joins a channel
- **THEN** they begin receiving that channel's new messages in real time

#### Scenario: Removal ends delivery immediately

- **WHEN** a user is removed from a channel while connected
- **THEN** they stop receiving that channel's messages without needing to reconnect
- **AND** subsequent requests for its content are denied

### Requirement: Messages

Users SHALL be able to post messages to channels they are members of, and to edit and delete their own messages subject to policy.

An edited message MUST be identifiable as edited, and a deleted message MUST leave a placeholder indicating removal rather than silently vanishing from other participants' views.

#### Scenario: Edit is marked

- **WHEN** a message is edited
- **THEN** all participants see the updated content marked as edited

#### Scenario: Deletion leaves evidence

- **WHEN** a message is deleted
- **THEN** its content is no longer retrievable
- **AND** participants see that a message was removed rather than the conversation appearing to have never contained it

#### Scenario: Posting to a non-member channel is refused

- **WHEN** a user posts to a channel they are not a member of
- **THEN** the request is denied

### Requirement: Threads

Any message SHALL be able to serve as the root of a thread, with replies grouped under it and kept out of the main channel timeline unless explicitly surfaced.

A thread MUST inherit the permissions and visibility of the channel containing its root message.

#### Scenario: Replies group under the root

- **WHEN** a user replies in a thread
- **THEN** the reply appears within that thread
- **AND** does not appear as a separate top-level message in the channel timeline

#### Scenario: Thread inherits channel visibility

- **WHEN** a thread exists in a private channel
- **THEN** only members of that channel can read or reply to it

#### Scenario: Thread participants are notified

- **WHEN** a reply is added to a thread
- **THEN** users participating in or following that thread are notified according to their preferences

### Requirement: Direct messages

Users SHALL be able to exchange direct messages with one or more other users without a channel being created by an administrator.

Direct message conversations MUST NOT be readable by administrators through ordinary administrative interfaces, and any operator capability to access them MUST be documented and audited.

#### Scenario: Direct conversation is private

- **WHEN** two users exchange direct messages
- **THEN** no other user, including administrators using ordinary interfaces, can read them

#### Scenario: Group direct message

- **WHEN** a user starts a direct conversation including several people
- **THEN** all named participants can read and reply
- **AND** participants can see who else is included

### Requirement: Disposable chats

The server SHALL support disposable conversations whose messages are removed automatically after a lifetime chosen at creation.

Expiry MUST be enforced by the server rather than relying on clients to hide expired content, and the lifetime MUST be visible to every participant before they post.

#### Scenario: Messages expire server-side

- **WHEN** a disposable chat's configured lifetime elapses
- **THEN** the messages are removed by the server
- **AND** they are not retrievable by any client, including one that was offline throughout

#### Scenario: Lifetime is disclosed

- **WHEN** a user views a disposable chat
- **THEN** its lifetime and the fact that content will be removed are displayed before they post

#### Scenario: Expiry is not client-dependent

- **WHEN** a client with cached content reconnects after expiry
- **THEN** the expired content is no longer served and the client discards it

### Requirement: Discovery

Users SHALL be able to browse and search the channels they are permitted to see, and search MUST never return a channel the requesting user is not permitted to discover.

#### Scenario: Search respects visibility

- **WHEN** a user searches for channels
- **THEN** results include only channels that user may discover

#### Scenario: Browsing is ordered usefully

- **WHEN** a user browses available channels
- **THEN** results are presented in a documented, stable order with the channel's purpose visible

### Requirement: Addressing

Channels and users SHALL be addressable in a form that includes their home server, so an address remains unambiguous when later exchanged between servers.

#### Scenario: Addresses include home server

- **WHEN** a channel or user identifier is presented in an API response
- **THEN** it identifies the home server in a form that stays unique across servers
