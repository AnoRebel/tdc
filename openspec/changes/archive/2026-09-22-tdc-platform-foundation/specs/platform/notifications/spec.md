## Purpose

Defines how users are told about activity that concerns them — in-application, on the web, and on the desktop — with controls precise enough that people migrating from always-on chat apps are not driven to disable notifications entirely.

## ADDED Requirements

### Requirement: Notification triggers

The server SHALL generate notifications for direct messages, mentions of the user, replies in threads the user participates in or follows, and activity in conversations the user has chosen to be notified about.

A user MUST NOT be notified of their own actions.

#### Scenario: Mention notifies

- **WHEN** a user is mentioned in a channel they are a member of
- **THEN** a notification is generated for that user according to their preferences

#### Scenario: Own action does not notify

- **WHEN** a user posts a message mentioning themselves
- **THEN** no notification is generated for that user

#### Scenario: Thread reply notifies participants

- **WHEN** a reply is posted in a thread
- **THEN** users participating in or following that thread are notified according to their preferences

### Requirement: Notification preferences

Users SHALL be able to set notification preferences globally and per conversation, including notifying for all activity, for mentions only, or not at all.

A per-conversation preference MUST override the global preference, and preferences MUST apply consistently across all of a user's devices.

#### Scenario: Per-conversation overrides global

- **WHEN** a user sets a conversation to mentions-only while their global setting is all activity
- **THEN** they are notified only for mentions in that conversation and for all activity elsewhere

#### Scenario: Muted conversation stays quiet

- **WHEN** a user mutes a conversation
- **THEN** no notification is delivered for it, while unread state still updates

#### Scenario: Preferences apply across devices

- **WHEN** a user changes a notification preference on one device
- **THEN** the change applies to their other devices

### Requirement: Web push

The server SHALL support web push notifications to browsers and installed web applications using an open, self-hostable mechanism requiring no paid service.

Push subscriptions MUST be per-device, revocable, and removed automatically when rejected as expired or invalid by the push service.

#### Scenario: Push arrives when application is closed

- **WHEN** a notification is generated for a user whose browser is closed but who has an active push subscription
- **THEN** the notification is delivered through the push service

#### Scenario: Push requires no paid service

- **WHEN** an operator enables web push
- **THEN** it functions using self-generated keys with no commercial service configured

#### Scenario: Invalid subscription is pruned

- **WHEN** the push service reports a subscription as expired or invalid
- **THEN** the server removes it and stops attempting delivery to it

#### Scenario: Push payload does not leak private content

- **WHEN** a push notification is generated for a private conversation and the user has chosen to hide content in notifications
- **THEN** the payload does not contain the message content

### Requirement: Desktop notifications

The desktop application SHALL raise native operating system notifications, respecting the operating system's own quiet or focus modes.

Activating a notification MUST bring the user to the relevant conversation.

#### Scenario: Native notification is raised

- **WHEN** a notification is generated while the desktop application is running in the background
- **THEN** a native notification appears

#### Scenario: Activation navigates

- **WHEN** the user activates a notification
- **THEN** the application comes to the foreground showing the conversation the notification concerns

#### Scenario: System quiet mode is respected

- **WHEN** the operating system is in a do-not-disturb state
- **THEN** the application does not override it

### Requirement: Suppression while present

A user SHALL NOT receive a push or desktop notification for a conversation they are actively viewing on any device.

Where a notification has already been delivered and the user reads the message elsewhere, the notification MUST be withdrawn where the platform supports it.

#### Scenario: Active view suppresses

- **WHEN** a message arrives in the conversation the user is currently viewing
- **THEN** no push or desktop notification is raised

#### Scenario: Read elsewhere withdraws

- **WHEN** a user reads a message on one device after a notification was delivered to another
- **THEN** the outstanding notification is withdrawn where the platform allows it

### Requirement: Status interaction

Notification delivery SHALL respect the user's configured status, so a status the operator has marked as suppressing notifications withholds them.

Users MUST be able to designate conversations that notify regardless of status.

#### Scenario: Suppressing status withholds

- **WHEN** a user holds a status configured to suppress notifications
- **THEN** notifications are withheld while unread state continues to update

#### Scenario: Priority conversation overrides status

- **WHEN** a user marks a conversation as always notifying and holds a suppressing status
- **THEN** notifications for that conversation are still delivered

### Requirement: Digests and batching

The server SHALL be able to batch notifications so that a burst of activity does not produce a corresponding burst of notifications.

Operators MUST be able to configure batching behaviour, and it MUST be possible to receive a periodic summary of missed activity instead of individual notifications.

#### Scenario: Burst is batched

- **WHEN** many messages arrive in quick succession in one conversation
- **THEN** the user receives a batched notification rather than one per message

#### Scenario: Summary replaces individual notifications

- **WHEN** a user opts into periodic summaries
- **THEN** they receive a summary of missed activity at the configured interval instead of individual notifications

### Requirement: Delivery is best-effort and non-blocking

Notification delivery SHALL NOT block or delay message delivery, and failure to deliver a notification MUST NOT cause loss of the underlying message or of unread state.

#### Scenario: Push failure does not affect messaging

- **WHEN** the push service is unreachable
- **THEN** messages are still delivered to connected clients and unread state is still updated
- **AND** the failure is logged without being surfaced to users as a messaging error
