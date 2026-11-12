## Purpose

Defines the tools a TDC server operator and their moderators use to keep a community healthy — content filtering, reporting, moderator actions, and retention — configured per server so each community can set its own standards.

## Requirements

### Requirement: Content filters

The server SHALL support operator-configured filters for undesired content, including a configurable word list. Filters MUST be per-server, disabled by default, and MUST NOT require a paid service.

A filter MUST be able to act by blocking the message, flagging it for moderator review, or masking the matched content, as configured.

#### Scenario: Filter blocks matching content

- **WHEN** a filter is configured to block and a message matching it is submitted
- **THEN** the message is refused
- **AND** the author is told their message was refused by a content policy

#### Scenario: Filter flags for review

- **WHEN** a filter is configured to flag and a matching message is submitted
- **THEN** the message is delivered
- **AND** it appears in the moderation queue for review

#### Scenario: Filters are per-server

- **WHEN** two servers configure different filters
- **THEN** each applies only its own configuration

#### Scenario: Filtering requires no external service

- **WHEN** an operator enables content filtering
- **THEN** it functions without any external or paid classification service

### Requirement: Media marking and handling

The server SHALL support marking media as sensitive, and MUST allow operators to require that such media is obscured until a viewer chooses to reveal it.

Automated classification, where configured, MUST be optional and MUST NOT be required for the feature to function.

#### Scenario: Sensitive media is obscured

- **WHEN** media marked sensitive is displayed and the server requires obscuring
- **THEN** it is obscured until the viewer chooses to reveal it

#### Scenario: Users can mark their own uploads

- **WHEN** a user marks their upload as sensitive
- **THEN** it is treated as sensitive for all viewers

#### Scenario: Automated classification is optional

- **WHEN** no automated classifier is configured
- **THEN** marking and obscuring still function through user and moderator action

### Requirement: Reporting

Users SHALL be able to report a message or another user to moderators, with a reason.

A report MUST reach the moderation queue, and the reporter MUST NOT be disclosed to the reported party.

#### Scenario: Report reaches moderators

- **WHEN** a user reports a message
- **THEN** the report appears in the moderation queue with the message, its context, and the stated reason

#### Scenario: Reporter is not disclosed

- **WHEN** a moderator acts on a report
- **THEN** the reported user is not told who reported them

#### Scenario: Reporting is rate-limited

- **WHEN** a user submits reports at an abusive rate
- **THEN** further reports from that user are throttled

### Requirement: Moderator actions

Moderators SHALL be able to remove messages, remove users from channels, suspend users from the server, and restrict a user's ability to post for a period.

Every moderator action MUST be recorded in the audit log with the actor, target, reason, and time, and MUST be reversible where the action is not inherently permanent.

#### Scenario: Message removal is recorded

- **WHEN** a moderator removes a message
- **THEN** the message content becomes unavailable
- **AND** the action is recorded in the audit log with the acting moderator and reason

#### Scenario: Suspension prevents access

- **WHEN** a user is suspended
- **THEN** their sessions are invalidated and they cannot sign in for the suspension's duration

#### Scenario: Temporary restriction expires

- **WHEN** a posting restriction's period elapses
- **THEN** the user regains the ability to post without moderator action

#### Scenario: Reversal is recorded

- **WHEN** a moderator reverses a previous action
- **THEN** the reversal is recorded in the audit log referencing the original action

### Requirement: Channel-level bans

A channel's moderators SHALL be able to ban a person from that channel, temporarily or permanently, independently of any server-level action.

A ban MUST remove the person from the channel, refuse their attempts to rejoin, and take effect on their open connections without waiting for a reconnection.

A temporary ban SHALL state when it ends, and MUST lift itself at that time without a moderator having to act again.

A ban MUST be recorded in the audit log with its reason and its duration, and MUST be reversible by any moderator of that channel.

Banning from one channel MUST NOT affect a person's membership of any other, so that a moderator of one space cannot reach beyond it.

#### Scenario: A banned person is removed and cannot return

- **WHEN** a moderator bans someone from a channel
- **THEN** they are removed from it
- **AND** an attempt to rejoin is refused

#### Scenario: A ban takes effect immediately

- **WHEN** someone is banned while connected
- **THEN** they stop receiving that channel's events without reconnecting

#### Scenario: A temporary ban expires on its own

- **WHEN** a temporary ban's period elapses
- **THEN** the person may rejoin without a moderator acting

#### Scenario: A ban is confined to its channel

- **WHEN** someone is banned from one channel
- **THEN** their membership of every other channel is unaffected

#### Scenario: A ban is recorded and reversible

- **WHEN** a ban is applied or lifted
- **THEN** the action, its reason and its duration are recorded in the audit log

### Requirement: Direct message availability

A person SHALL be able to say who may open a direct conversation with them: anyone on the server, only people sharing a channel with them, or nobody.

The server MUST enforce that choice, refusing a conversation the recipient's setting does not permit, and MUST NOT disclose which setting caused a refusal — a refusal that distinguishes "closed to everyone" from "closed to you" tells a stranger something about the person refusing.

Changing the setting MUST NOT close conversations that already exist, since a person who has already been spoken to has already been reachable.

A moderator's ability to contact someone about a moderation matter MUST NOT be blocked by this setting.

#### Scenario: A closed inbox refuses a stranger

- **WHEN** someone opens a direct conversation with a person who accepts none
- **THEN** it is refused

#### Scenario: Sharing a channel is enough, when that is the setting

- **WHEN** someone in a shared channel opens a conversation with a person set to allow that
- **THEN** it is permitted

#### Scenario: A refusal discloses nothing

- **WHEN** a conversation is refused
- **THEN** the response does not reveal whether the person is closed to everyone or only to this sender

#### Scenario: Existing conversations survive a change

- **WHEN** a person narrows who may contact them
- **THEN** conversations already open remain usable

### Requirement: Moderation queue

The server SHALL present flagged content and reports in a queue for moderators, showing enough surrounding context to judge, and recording the outcome of each item.

#### Scenario: Queue provides context

- **WHEN** a moderator opens a queued item
- **THEN** they see the reported content, the conversation around it, and the reason it was queued

#### Scenario: Outcome is recorded

- **WHEN** a moderator resolves a queued item
- **THEN** the resolution and its author are recorded and the item leaves the queue

### Requirement: Retention

Operators SHALL be able to configure how long messages and attachments are retained, globally and per channel, after which content is removed automatically.

Retention policy MUST be visible to the members of an affected conversation, and changing it MUST NOT retroactively destroy content without a recorded administrative action.

#### Scenario: Content expires per policy

- **WHEN** a retention period configured for a channel elapses for a message
- **THEN** the message and its attachments are removed

#### Scenario: Policy is disclosed to members

- **WHEN** a user views a channel with a retention policy
- **THEN** the policy is visible to them

#### Scenario: Shortening policy is a recorded action

- **WHEN** an operator shortens a retention period such that existing content becomes eligible for removal
- **THEN** the change is recorded in the audit log with the actor and both values

### Requirement: User-level controls

Users SHALL be able to block another user, after which they do not receive that user's direct messages or see their messages in shared channels.

Blocking MUST NOT be disclosed to the blocked user, and MUST NOT prevent moderators from seeing the full conversation.

#### Scenario: Blocked user's content is hidden

- **WHEN** a user blocks another
- **THEN** the blocking user no longer sees the blocked user's messages
- **AND** direct messages from the blocked user are not delivered

#### Scenario: Blocking is not disclosed

- **WHEN** a blocked user sends a direct message
- **THEN** they are not informed that they have been blocked

#### Scenario: Moderators retain full view

- **WHEN** a moderator reviews a conversation involving a block
- **THEN** they see all messages regardless of the block
