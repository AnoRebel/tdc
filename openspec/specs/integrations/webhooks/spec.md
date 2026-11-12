## Purpose

Defines how external systems send events into TDC and receive events from it, so that a community's existing tools — version control, continuous integration, deployment, monitoring — can participate in its conversations without a bespoke integration for each.

## Requirements

### Requirement: Incoming webhooks

The server SHALL allow a user with permission to create an incoming webhook bound to a specific channel, which external systems can call to post a message.

Each webhook MUST have its own revocable credential, and revoking it MUST take effect immediately.

#### Scenario: External system posts a message

- **WHEN** an external system calls an incoming webhook with valid credentials and a message
- **THEN** the message is posted to the bound channel, attributed to the webhook

#### Scenario: Webhook cannot post elsewhere

- **WHEN** a request attempts to post to a channel other than the one the webhook is bound to
- **THEN** the request is refused

#### Scenario: Revocation is immediate

- **WHEN** a webhook's credential is revoked
- **THEN** subsequent calls using it are refused

#### Scenario: Creation and revocation are audited

- **WHEN** a webhook is created or revoked
- **THEN** the action is recorded in the audit log with the actor

### Requirement: Outgoing webhooks

The server SHALL allow subscribing an external endpoint to selected event types, delivering those events as they occur.

A subscription MUST be limited to events the subscribing user is permitted to see, and MUST stop delivering if that permission is lost.

#### Scenario: Subscribed events are delivered

- **WHEN** an event matching a subscription occurs
- **THEN** it is delivered to the subscribed endpoint

#### Scenario: Subscription cannot exceed the subscriber's access

- **WHEN** a user subscribes to events for a channel they cannot read
- **THEN** the subscription is refused

#### Scenario: Lost permission stops delivery

- **WHEN** the subscribing user loses access to a channel or project
- **THEN** delivery of its events to their subscription stops

### Requirement: Subscription scopes

An outgoing subscription SHALL listen to exactly one of: a channel, a project, or the whole server.

A channel or project subscription MUST be refused, as though it did not exist, when its owner cannot read the channel or see the project. An event type MUST be refused where it cannot happen — an issue's events on a channel, a message's on a project — and a channel named as a project, or a project as a channel, MUST be refused. A server subscription MUST receive an event in a room only while its owner can read that room, checked when the event is queued and again when it is sent.

#### Scenario: A project's events reach a subscriber who may see it

- **WHEN** somebody who can see a project subscribes to it and an issue in it is filed, moved, closed, reopened or commented on
- **THEN** each is delivered with the project and, for a comment, the issue named in the payload

#### Scenario: A private project is not heard from outside

- **WHEN** somebody outside a private project subscribes to it, or subscribes to the whole server
- **THEN** the first is refused as not found, and the second receives nothing about the project

#### Scenario: A server subscription hears only readable rooms

- **WHEN** a private channel is made, or a message posted in it, by somebody else
- **THEN** a server subscription whose owner is not in it receives nothing, while a public channel's creation reaches them

#### Scenario: A type that cannot happen there is refused

- **WHEN** a subscription asks for an issue's events on a channel, or a message's events on a project
- **THEN** it is refused as a bad request

### Requirement: Event catalogue

The server SHALL publish the catalogue of event types a subscription can ask for, each with its group, the scopes it can be asked for in, any permission it needs, and whether the caller could ask for it now. Every type in the catalogue MUST be documented with what it carries.

The catalogue MUST include messages, membership, channel lifecycle, calls, directory engagements, projects and their members and states, issues and their changes, cycles and milestones, and the server's people. A comment on an issue, an issue being closed and an issue being reopened MUST be offered as types of their own, named by the server from the event they are, and a subscriber asking for both names MUST receive both.

Every event the server stores MUST be offered to subscriptions from the one place it is stored, so that no route that stores one can omit it.

#### Scenario: Joins and leaves are delivered

- **WHEN** somebody joins or leaves a channel with a subscription for those types
- **THEN** both are delivered

#### Scenario: The catalogue says what is available

- **WHEN** somebody without a type's permission reads the catalogue
- **THEN** that type is listed as not available to them

### Requirement: Events that belong to no room

The server SHALL deliver a new account and a change of somebody's role to server subscriptions whose owner holds `user.read.all`, checked when queued and again when sent. Such an event MUST NOT carry anybody's email address, and MUST NOT be asked for without the permission.

#### Scenario: A new account is announced to whoever may read every account

- **WHEN** an account is created
- **THEN** a server subscription of somebody holding `user.read.all` receives it with the account's name and address, and no email address

#### Scenario: Losing the permission stops it

- **WHEN** a subscriber loses `user.read.all` and an account is then created
- **THEN** nothing is delivered to them

#### Scenario: Asking without the permission is refused

- **WHEN** somebody without `user.read.all` subscribes to new accounts
- **THEN** the subscription is refused as forbidden

### Requirement: Delivery signing

Every outgoing delivery SHALL be signed so the receiver can verify it originated from the server and was not altered.

The signature MUST cover the payload and a timestamp so a captured delivery cannot be replayed indefinitely.

#### Scenario: Receiver can verify origin

- **WHEN** an outgoing delivery is received
- **THEN** the receiver can verify its signature using the documented scheme and the configured secret

#### Scenario: Altered payload fails verification

- **WHEN** a delivery's payload is altered in transit
- **THEN** signature verification fails

#### Scenario: Replay is detectable

- **WHEN** a delivery is replayed outside its timestamp window
- **THEN** the receiver can detect it using the documented scheme

### Requirement: Delivery reliability

The server SHALL retry failed deliveries with backoff up to a documented limit, and MUST record the outcome of each attempt.

A persistently failing subscription MUST be disabled and its owner informed, rather than retried indefinitely.

#### Scenario: Temporary failure is retried

- **WHEN** a delivery fails due to a temporary error
- **THEN** it is retried with increasing delay up to the documented limit

#### Scenario: Persistent failure disables the subscription

- **WHEN** a subscription fails continuously beyond the documented limit
- **THEN** it is disabled and its owner is informed

#### Scenario: Delivery history is visible

- **WHEN** a subscription owner inspects it
- **THEN** they see recent delivery attempts with their outcomes and response codes

#### Scenario: Slow endpoint does not degrade the server

- **WHEN** a subscribed endpoint responds slowly or not at all
- **THEN** messaging and other capabilities are unaffected

### Requirement: Outbound request safety

Outgoing deliveries SHALL be subject to the same restrictions as other server-initiated requests, so a subscription cannot be used to reach hosts that the server can access but an outside user cannot.

#### Scenario: Internal addresses are refused

- **WHEN** a subscription names an endpoint resolving to a loopback, link-local, or private network address
- **THEN** the subscription is refused unless the operator has explicitly permitted that range

#### Scenario: Redirects do not bypass restrictions

- **WHEN** a delivery is redirected to a restricted address
- **THEN** the redirect is not followed

### Requirement: Rate limiting

Both incoming and outgoing webhook traffic SHALL be rate-limited per webhook and per subscription, with limits configurable by the operator.

Exceeding a limit MUST be reported clearly rather than silently dropping events.

#### Scenario: Excessive incoming calls are limited

- **WHEN** an incoming webhook is called beyond its configured rate
- **THEN** further calls are refused with a response indicating the limit and when to retry

#### Scenario: Exceeded limits are visible

- **WHEN** events are dropped or delayed because of a rate limit
- **THEN** the webhook's owner can see that this occurred

### Requirement: Payload format is documented and versioned

Event payloads SHALL follow a documented, versioned schema, and a change that would break existing consumers MUST introduce a new version rather than altering the existing one.

#### Scenario: Payload matches published schema

- **WHEN** an event is delivered
- **THEN** its payload conforms to the published schema for its declared version

#### Scenario: Breaking change is a new version

- **WHEN** a payload change would break existing consumers
- **THEN** it is published as a new version and the previous version continues to be delivered to existing subscriptions

### Requirement: Message formatting from integrations

Messages posted by integrations SHALL support structured formatting so that build results, alerts, and similar content are readable, and MUST be clearly attributed to the integration rather than to a person.

Content from integrations MUST be subject to the same sanitisation as user content.

#### Scenario: Integration message is attributed

- **WHEN** an integration posts a message
- **THEN** it is displayed as originating from that integration, not from a user account

#### Scenario: Integration content is sanitised

- **WHEN** an integration posts content containing markup or scripts
- **THEN** it is sanitised exactly as user-submitted content would be
