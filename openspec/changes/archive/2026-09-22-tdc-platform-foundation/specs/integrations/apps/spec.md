## Purpose

Defines third-party applications and bots on a TDC server — how they are installed, what they are permitted to do, and how their access is scoped, granted, and revoked — so a server can be extended without every extension holding the keys to everything.

## ADDED Requirements

### Requirement: Application registration

An operator SHALL be able to register a third-party application on their server, recording its name, its purpose, and the permissions it requests.

An application MUST NOT gain any access until an authorised user has approved it.

#### Scenario: Registration alone grants nothing

- **WHEN** an application is registered but not yet approved
- **THEN** it cannot read or write any data

#### Scenario: Approval is explicit and informed

- **WHEN** a user approves an application
- **THEN** they are shown exactly which permissions it will hold before approving

#### Scenario: Registration is audited

- **WHEN** an application is registered, approved, or removed
- **THEN** the action is recorded in the audit log with the actor

### Requirement: Scoped access

An application's access SHALL be limited to the permissions it was granted, and MUST NOT inherit the full authority of the user who approved it.

An application MUST be restrictable to specific channels or projects.

#### Scenario: Application cannot exceed its grant

- **WHEN** an application attempts an action outside its granted permissions
- **THEN** the action is refused

#### Scenario: Application does not inherit user authority

- **WHEN** an administrator approves an application requesting only the ability to post messages
- **THEN** the application cannot perform administrative actions despite being approved by an administrator

#### Scenario: Channel restriction is enforced

- **WHEN** an application is restricted to certain channels and attempts to act in another
- **THEN** the action is refused

### Requirement: Credentials

Each application SHALL hold its own credentials, distinct from any user's, which are revocable independently and MUST be displayed only once at creation.

Revoking an application's credentials MUST immediately end its access.

#### Scenario: Credential is shown once

- **WHEN** an application's credential is created
- **THEN** it is displayed once and cannot be retrieved again afterwards

#### Scenario: Revocation is immediate

- **WHEN** an application's credentials are revoked
- **THEN** its subsequent requests are refused

#### Scenario: Rotation without downtime

- **WHEN** an application's credential is rotated
- **THEN** a documented overlap allows the application to adopt the new credential before the old one stops working

### Requirement: Bots

An application SHALL be able to act as a bot with its own identity in conversations, clearly distinguishable from a human user.

A bot MUST be subject to the same permissions, rate limits, and moderation as any other participant.

#### Scenario: Bot is distinguishable

- **WHEN** a bot posts a message
- **THEN** it is displayed as a bot, not as a human user

#### Scenario: Bot is moderatable

- **WHEN** a moderator removes a bot's message or restricts the bot
- **THEN** the action takes effect exactly as it would for a user

#### Scenario: Bot cannot bypass permissions

- **WHEN** a bot attempts to read a channel it has not been granted access to
- **THEN** the request is refused

### Requirement: Rate limits and isolation

Applications SHALL be rate-limited independently of users, and an application's behaviour MUST NOT be able to degrade service for people.

#### Scenario: Misbehaving application is contained

- **WHEN** an application issues requests at an abusive rate
- **THEN** its requests are throttled
- **AND** human users' experience is unaffected

#### Scenario: Application failure is isolated

- **WHEN** an application errors repeatedly
- **THEN** the server continues operating normally and the failures are recorded

### Requirement: Visibility of installed applications

Users SHALL be able to see which applications are installed on their server, what permissions each holds, and when each was approved.

Where an application can read a conversation, that MUST be discoverable by the conversation's participants.

#### Scenario: Installed applications are listed

- **WHEN** a user views the server's applications
- **THEN** each is listed with its permissions and approval date

#### Scenario: Participants can see who can read

- **WHEN** an application has access to a channel
- **THEN** members of that channel can discover this

### Requirement: Removal

Removing an application SHALL immediately end its access and MUST offer to remove or retain the content it created, at the operator's choice.

#### Scenario: Removal ends access

- **WHEN** an application is removed
- **THEN** its credentials stop working immediately

#### Scenario: Content disposition is chosen

- **WHEN** an application that posted messages is removed
- **THEN** the operator is asked whether to retain or remove that content, and the choice is applied

### Requirement: External services require disclosure

An application that sends data to an external service SHALL declare this, and users MUST be informed before approving it.

Any such application MUST be optional, and the server MUST function fully without it.

#### Scenario: External data flow is disclosed

- **WHEN** an application that transmits data to an external service is presented for approval
- **THEN** the approver is told which data leaves the server and where it goes

#### Scenario: Server functions without external applications

- **WHEN** no external-service applications are installed
- **THEN** every core capability of the server continues to function
