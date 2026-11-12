## Purpose

Defines programmable messages that carry actions — buttons, menus, and forms — which people can invoke to trigger behaviour in an integration, so that approvals, assignments, deployments, and similar decisions happen in the conversation rather than in another tool.

## ADDED Requirements

### Requirement: Messages can carry actions

An integration SHALL be able to post a message carrying interactive elements, including buttons, selection menus, and short input fields, described in a documented, versioned schema.

The server MUST validate an interactive message against that schema and refuse one that does not conform, rather than rendering it partially.

#### Scenario: Interactive message is presented

- **WHEN** an integration posts a message carrying buttons
- **THEN** recipients see the message with those buttons presented as actionable controls

#### Scenario: Malformed payload is refused

- **WHEN** an integration posts an interactive message that does not conform to the schema
- **THEN** the message is refused with an explanation and is not posted

#### Scenario: Unknown element type degrades safely

- **WHEN** a client receives an interactive element type it does not understand
- **THEN** it presents the message's text without the unsupported element rather than failing to render

### Requirement: Invoking an action

When a user activates an interactive element, the server SHALL deliver the invocation to the owning integration, identifying the action, the message, the channel, and the invoking user.

The invocation MUST be signed by the server exactly as other outgoing deliveries are, so the integration can verify it is genuine.

#### Scenario: Activation reaches the integration

- **WHEN** a user activates a button on an interactive message
- **THEN** the owning integration receives an invocation naming the action, message, channel, and user

#### Scenario: Invocation is verifiable

- **WHEN** an integration receives an invocation
- **THEN** it can verify the server's signature using the documented scheme

#### Scenario: Invocation for a removed integration fails cleanly

- **WHEN** a user activates an action whose integration has been removed
- **THEN** the user is told the action is no longer available and no request is sent

### Requirement: Authorization of the invoker

The server SHALL evaluate permissions against the user who invoked an action, not against the integration that posted the message.

An integration MUST be able to restrict an action to users holding a stated permission or role, and the server MUST enforce that restriction before delivering the invocation.

#### Scenario: Unauthorized user cannot invoke

- **WHEN** a user without the required permission activates a restricted action
- **THEN** the invocation is refused and not delivered to the integration

#### Scenario: Invoker identity is not spoofable

- **WHEN** an invocation is delivered
- **THEN** the identity it carries is the authenticated invoking user, and cannot be set by the client or the integration

#### Scenario: Channel access is required

- **WHEN** a user who cannot read the containing channel attempts to invoke one of its actions
- **THEN** the invocation is refused

### Requirement: Responding to an invocation

An integration SHALL be able to respond to an invocation by updating the originating message, posting a new message, or replying privately to the invoking user only.

An update MUST replace the message's interactive elements atomically, so a decision cannot be recorded twice through a stale view.

#### Scenario: Message is updated in place

- **WHEN** an integration responds by updating the originating message
- **THEN** all viewers see the updated content and controls

#### Scenario: Private response is seen only by the invoker

- **WHEN** an integration responds privately
- **THEN** only the invoking user sees the response

#### Scenario: Stale activation is rejected

- **WHEN** a user activates a control that has since been replaced by an update
- **THEN** the invocation is refused as stale rather than being processed a second time

#### Scenario: Slow integration does not block the user

- **WHEN** an integration does not respond within the documented timeout
- **THEN** the user is told the action did not complete, and the message is not left appearing to have succeeded

### Requirement: Single-use and expiring actions

Interactive elements SHALL be able to declare that they may be invoked only once, or only before a stated expiry.

The server MUST enforce both, so an integration does not have to defend against replay itself.

#### Scenario: Single-use action cannot be repeated

- **WHEN** a single-use action has been invoked successfully
- **THEN** further activations of it are refused

#### Scenario: Expired action is refused

- **WHEN** an action is activated after its stated expiry
- **THEN** the invocation is refused and the user is told it has expired

### Requirement: Actions are auditable

Every invocation of an interactive action SHALL be recorded, identifying the action, the invoking user, the integration, and the time.

Where an action causes a change of state on the server, that change MUST be recorded in the audit log as an action by the invoking user.

#### Scenario: Invocation is recorded

- **WHEN** a user invokes an action
- **THEN** the invocation is recorded with the user, action, integration, and time

#### Scenario: Resulting change is attributed to the person

- **WHEN** an invoked action changes server state
- **THEN** the audit log attributes the change to the invoking user, noting the integration through which it was made

### Requirement: Rate limiting and abuse resistance

Invocations SHALL be rate-limited per user and per integration, and an integration MUST NOT be able to use interactive messages to generate unbounded notification volume.

#### Scenario: Rapid invocation is throttled

- **WHEN** a user activates actions at an abusive rate
- **THEN** further invocations are refused until the rate falls

#### Scenario: Interactive messages respect notification preferences

- **WHEN** an integration posts interactive messages
- **THEN** they are subject to the same notification preferences, muting, and batching as any other message

### Requirement: Programmatic conversation

Integrations SHALL be able to conduct a multi-step exchange with a user, carrying context between steps, without that context being readable by other users.

Any state an integration keeps against a conversation MUST be discarded when the conversation or the integration is removed.

#### Scenario: Multi-step exchange retains context

- **WHEN** a user responds to a prompt from an integration and the integration asks a follow-up
- **THEN** the integration receives enough context to associate the two steps

#### Scenario: Context is private

- **WHEN** an integration keeps state for an exchange with one user
- **THEN** no other user can read that state through any interface

#### Scenario: Removal discards state

- **WHEN** an integration or the containing conversation is removed
- **THEN** the retained state is discarded
