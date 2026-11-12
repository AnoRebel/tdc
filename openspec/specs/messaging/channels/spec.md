## Purpose

Defines the spaces in which TDC conversation happens — servers, channels, threads, direct messages, and disposable chats — including how they are created, joined, discovered, archived, and how membership governs who can see what.

## Requirements

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

### Requirement: Message formatting

A message body SHALL be stored as the Markdown source its author wrote, and clients SHALL render a safe subset of it: bold, italic, strikethrough, inline code, fenced code blocks with an optional language, block quotes, ordered and unordered lists, and links. Headings MUST render no larger than body text. Raw HTML in a body MUST be shown as text and never interpreted; Markdown images MUST NOT be fetched; a link MUST point only to `http`, `https` or `mailto` addresses and MUST open with `rel="noopener noreferrer nofollow"`. Rendered output MUST pass through a sanitiser that admits only the elements the subset produces.

Addresses, email addresses and phone numbers in a body's text SHALL continue to be detected and made clickable as they were before formatting existed, except inside code. Mentions and channel names SHALL read as before; a mention inside code MUST NOT notify anybody.

Everything that cannot show formatting — notification previews, push payloads, desktop banners, the search index and its highlighted excerpts, and quotations of a message in the interface — MUST show the body as plain text with the formatting markers removed and the words, code contents and link addresses kept. The server and every client MUST strip formatting by the same rules.

A person SHALL be able to turn formatting off for themselves, after which bodies show exactly as typed.

#### Scenario: Formatting renders

- **WHEN** a message contains `**bold**`, a fenced code block labelled `go`, and a list
- **THEN** readers see bold text, a code block with its language, a copy control and horizontal scrolling for long lines, and a list

#### Scenario: Markup in a message stays text

- **WHEN** a message contains `<script>`, an HTML element with an event handler, or a link to a `javascript:` address
- **THEN** none of it executes, and the HTML is shown as the characters that were typed

#### Scenario: Plain-text surfaces strip formatting

- **WHEN** a message `**deploy** is done` notifies somebody, is pushed to a device, or is found by search
- **THEN** the preview, payload and excerpt read `deploy is done`

#### Scenario: A mention inside code notifies nobody

- **WHEN** a message contains `@amina` only inside a code span or block
- **THEN** amina is not notified

#### Scenario: Formatting off shows the source

- **WHEN** a person turns off "Show formatting in messages"
- **THEN** every message shows its source text, with links still clickable

### Requirement: Threads

Any message SHALL be able to serve as the root of a thread, with replies grouped under it and kept out of the main channel timeline unless explicitly surfaced.

A thread MUST inherit the permissions and visibility of the channel containing its root message.

A reply SHALL be a message in every respect. Posting one MUST accept the same inputs as posting to the channel — text, uploaded attachments, an idempotency token, and a program's buttons — and MUST pass the same checks: the posting permission, channel bans and posting restrictions, the content filter, attachment validation, and any rate limit. Everything that can be done to a message SHALL be possible on a reply: formatting, mentions, reactions, editing and deleting, reporting, pinning, forwarding, filing an issue from it, giving it a lifetime, marking its files sensitive, and pressing its buttons. A reply SHALL be indexed for search, and a reply can be scheduled to send later. An edit, deletion or update of a reply SHALL belong to the thread, not the channel timeline.

A person SHALL follow a thread automatically when they start it or reply in it, and MAY follow or stop following any thread explicitly; an explicit choice is not overridden by later participation. Each person's read position in a thread SHALL be recorded separately from the channel's, and replies SHALL NOT count towards the channel's unread count.

#### Scenario: A reply carries what a message carries

- **WHEN** a member replies in a thread with text and uploaded files
- **THEN** the reply is stored with those files attached, exactly as a message would be
- **AND** a reply naming files that are not the sender's uploads in that room is refused before anything is stored

#### Scenario: A reply passes the checks a message does

- **WHEN** a person who is banned from a channel, restricted from posting, or whose text the content filter blocks, replies in one of its threads
- **THEN** the reply is refused for the same reason a message would be

#### Scenario: Editing a reply stays in its thread

- **WHEN** somebody edits or deletes their reply
- **THEN** the thread shows the change
- **AND** the channel timeline shows nothing of it

#### Scenario: Unread replies in a followed thread

- **WHEN** a reply is posted in a thread a person follows, and they have not opened the thread since
- **THEN** the channel shows that the thread has new replies for them
- **AND** opening the thread marks where the new replies begin and records them as read

#### Scenario: Opening a reply from elsewhere

- **WHEN** a search result, a pin or a notification names a reply
- **THEN** the client opens the reply's thread with the reply brought into view

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
- **AND** a member who neither follows the thread nor is mentioned in the reply is not notified of it, whatever their channel notification level

#### Scenario: Leaving a thread

- **WHEN** a person stops following a thread
- **THEN** further replies in it do not notify them unless they are mentioned
- **AND** replying in it again does not make them follow it again

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

### Requirement: Disappearing messages

A participant SHALL be able to give an individual message a lifetime, independently of whether the conversation itself is disposable, and the server MUST remove the message when that lifetime elapses.

A lifetime SHALL be expressible either as a duration from sending, or as expiry once every other participant has seen it, or as expiry a chosen duration after each recipient first sees it.

Expiry MUST be enforced by the server: an expired message MUST NOT be served to any client, including one that was offline throughout its lifetime and one requesting a range that would have contained it.

A message's lifetime MUST be visible to every participant while the message is still readable, so that nobody is surprised by its removal, and MUST NOT be extendable after sending — only shortened, and only by its sender or a moderator.

An expired message SHALL leave the same placeholder a deleted message leaves, so that the conversation does not appear never to have contained it, and the placeholder MUST NOT disclose the removed content.

#### Scenario: A message expires on its own timer

- **WHEN** a message is sent with a lifetime and that lifetime elapses
- **THEN** the server removes its content
- **AND** it is not served to any client thereafter

#### Scenario: A message expires after being seen

- **WHEN** a message is sent to expire on viewing and every other participant has seen it
- **THEN** the server removes its content within a bounded time

#### Scenario: Expiry survives a client being offline

- **WHEN** a client that was offline throughout a message's lifetime reconnects
- **THEN** the expired message is not delivered
- **AND** any copy the client already held is discarded

#### Scenario: An expired message leaves a placeholder

- **WHEN** a message expires
- **THEN** participants see that a message was there and has gone
- **AND** the placeholder does not disclose what it said

#### Scenario: A lifetime is disclosed before expiry

- **WHEN** a participant reads a message that will expire
- **THEN** its remaining lifetime is visible to them

#### Scenario: A lifetime cannot be extended

- **WHEN** a sender attempts to lengthen a lifetime after sending
- **THEN** the change is refused

#### Scenario: Attachments expire with their message

- **WHEN** a message carrying attachments expires
- **THEN** those attachments are removed and no longer retrievable

### Requirement: Invitations

A channel SHALL be able to require an invitation to join, and its moderators SHALL be able to issue invitations as a link, as a short code, or as both for the same invitation.

An invitation MUST carry limits its issuer chooses: when it expires, how many times it may be used, and whether it admits anyone holding it or only a named person. An invitation with no remaining uses, or past its expiry, MUST be refused — and refusing MUST NOT disclose whether the invitation ever existed, so that guessing cannot be used to enumerate them.

An invitation's secret MUST be long enough that guessing is impractical, and MUST be generated from a cryptographically secure source. A code short enough to be read aloud MUST compensate with a stricter rate limit on attempts, since it is necessarily easier to guess than a link.

A moderator SHALL be able to revoke an invitation before it expires, taking effect immediately, and to see which invitations exist, who issued each, and how many times each has been used. Issuing, using and revoking MUST each be recorded in the audit log.

Accepting an invitation MUST grant membership of that channel only, and MUST NOT confer any permission beyond it.

Where the server itself is invitation-only, the same rules apply to invitations to the server, and accepting one MUST additionally permit registration.

#### Scenario: An invitation admits its holder

- **WHEN** someone opens a valid invitation link or enters its code
- **THEN** they may join that channel

#### Scenario: An expired invitation is refused

- **WHEN** an invitation is used after its expiry
- **THEN** it is refused

#### Scenario: A used-up invitation is refused

- **WHEN** an invitation that has reached its use limit is used again
- **THEN** it is refused

#### Scenario: A refusal discloses nothing

- **WHEN** an invalid, expired, revoked or never-existent invitation is presented
- **THEN** the refusals are indistinguishable from one another

#### Scenario: Revocation is immediate

- **WHEN** a moderator revokes an invitation
- **THEN** it stops working at once, including for someone who already holds the link

#### Scenario: An invitation grants nothing beyond the channel

- **WHEN** someone joins through an invitation
- **THEN** they hold ordinary membership of that channel and no other permission

#### Scenario: Codes are rate limited

- **WHEN** codes are guessed repeatedly against a channel
- **THEN** attempts are refused after a documented threshold

#### Scenario: Invitations are auditable

- **WHEN** an invitation is issued, used or revoked
- **THEN** the action, its issuer and its limits are recorded in the audit log

### Requirement: Member lists respect each person's privacy

A channel's members SHALL be able to see who else is in it, and each person SHALL control which of their own details that list discloses.

A person's display name and address are always shown, because they are how a message is attributed and a conversation is opened; everything beyond that — presence, status, the time they were last seen, their profile details — is theirs to withhold.

The server MUST omit withheld details from the response rather than sending them for a client to hide, since a detail sent is a detail disclosed however the client behaves.

A moderator viewing the list in a moderation context MAY see what is withheld, and that viewing MUST be recorded in the audit log.

#### Scenario: A member list shows who is present

- **WHEN** a member views a channel's member list
- **THEN** the other members are listed with their names and addresses

#### Scenario: Withheld details are not sent

- **WHEN** a person has chosen not to show their presence
- **THEN** the member list sent to others omits it entirely rather than marking it hidden

#### Scenario: A non-member cannot enumerate members

- **WHEN** someone who is not in a private channel requests its member list
- **THEN** the request is refused without revealing whether the channel exists

#### Scenario: Moderator access is recorded

- **WHEN** a moderator views details a person has withheld
- **THEN** the access is recorded in the audit log

### Requirement: Discovery

Users SHALL be able to browse and search the channels they are permitted to see, and search MUST never return a channel the requesting user is not permitted to discover.

#### Scenario: Search respects visibility

- **WHEN** a user searches for channels
- **THEN** results include only channels that user may discover

#### Scenario: Browsing is ordered usefully

- **WHEN** a user browses available channels
- **THEN** results are presented in a documented, stable order with the channel's purpose visible

### Requirement: Scheduled messages

Users SHALL be able to write a message to be sent at a later time, and to list, reword, move, send immediately or cancel their own messages that have not yet been sent. A message being sent at that moment MUST NOT be changed or cancelled, and a message MUST NOT be sent twice.

#### Scenario: Only the author changes a scheduled message

- **WHEN** a user tries to change, send or cancel another user's scheduled message
- **THEN** the request is refused as though the message did not exist

#### Scenario: Sending now meets the checks sending later would

- **WHEN** a user sends a scheduled message immediately
- **THEN** it is sent as an ordinary message, subject to the same posting checks, and leaves the list of scheduled messages

#### Scenario: A message being sent is left alone

- **WHEN** a delivery pass has claimed a scheduled message
- **THEN** cancelling, changing or sending it now is refused

### Requirement: Personal conversation list

Users SHALL be able to pin conversations to the top of their own list, in an order they choose, without that being visible to anybody else.

#### Scenario: Pins are private and ordered

- **WHEN** a user pins conversations and rearranges them
- **THEN** their list shows them first in that order on every device
- **AND** no other user can observe the pins

### Requirement: Addressing

Channels and users SHALL be addressable in a form that includes their home server, so an address remains unambiguous when later exchanged between servers.

#### Scenario: Addresses include home server

- **WHEN** a channel or user identifier is presented in an API response
- **THEN** it identifies the home server in a form that stays unique across servers
