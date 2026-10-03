## Purpose

Defines how a client and a developer move from first contact to agreed work — enquiry, scoping, proposal, negotiation, and a recorded agreement — so that the terms both sides settled on are captured rather than scattered through chat history.

## Requirements

### Requirement: Enquiries

A client SHALL be able to send an enquiry to a developer or company from their profile, describing the work and its context.

Enquiries MUST be subject to rate limiting and to the recipient's contact preferences, so the directory cannot be used for unsolicited bulk approaches.

#### Scenario: Enquiry reaches the recipient

- **WHEN** a client sends an enquiry to an available developer
- **THEN** the developer is notified and can view it

#### Scenario: Recipient preferences are honoured

- **WHEN** a user has indicated they are not accepting enquiries
- **THEN** the enquiry is refused and the sender is told

#### Scenario: Bulk approaches are limited

- **WHEN** a client sends enquiries at an abusive rate
- **THEN** further enquiries are refused

### Requirement: Engagement lifecycle

An engagement SHALL progress through documented states from enquiry through discussion, proposal, agreement, active work, and completion, with either party able to withdraw before agreement.

The current state MUST be visible to both parties at all times, and every transition MUST be recorded with the actor and time.

#### Scenario: State is shared and visible

- **WHEN** an engagement changes state
- **THEN** both parties see the same current state

#### Scenario: Withdrawal before agreement

- **WHEN** either party withdraws before an agreement is recorded
- **THEN** the engagement is closed as withdrawn and no agreement exists

#### Scenario: Transitions are recorded

- **WHEN** an engagement transitions between states
- **THEN** the transition is recorded with who caused it and when

### Requirement: Private discussion

Every engagement SHALL have a private conversation between its participants, subject to the same messaging, attachment, and retention rules as other conversations.

No one outside the engagement may read it, and the platform MUST NOT expose it in directory browsing or search.

#### Scenario: Discussion is private to participants

- **WHEN** an engagement conversation exists
- **THEN** only its participants can read it

#### Scenario: Engagement discussion is not discoverable

- **WHEN** any user searches or browses
- **THEN** engagement conversations they are not party to do not appear

### Requirement: Scope and proposals

A developer SHALL be able to record a proposal against an engagement, stating the scope of work, the deliverables, a timeline, and a price.

A proposal MUST be immutable once sent; revising it MUST create a new version, and the full sequence of versions MUST remain visible to both parties.

#### Scenario: Proposal is recorded and visible

- **WHEN** a developer sends a proposal
- **THEN** the client can view its scope, deliverables, timeline, and price

#### Scenario: Revision creates a version

- **WHEN** a developer revises a sent proposal
- **THEN** a new version is created
- **AND** both parties can still see the previous versions

#### Scenario: Sent proposals cannot be rewritten

- **WHEN** a party attempts to alter the content of an already-sent proposal
- **THEN** the attempt is refused

### Requirement: Negotiation

Parties SHALL be able to respond to a proposal by accepting it, declining it, or requesting changes with comments attached to specific parts of it.

An engagement MUST NOT advance to agreement without an explicit acceptance recorded from both parties.

#### Scenario: Change request is attributable

- **WHEN** a client requests changes to part of a proposal
- **THEN** the request is recorded against that part and the developer is notified

#### Scenario: Agreement requires both parties

- **WHEN** only one party has accepted a proposal
- **THEN** the engagement does not advance to agreement

#### Scenario: Decline closes cleanly

- **WHEN** a party declines a proposal
- **THEN** the engagement records the decline and either party may propose again or withdraw

### Requirement: Agreement record

When both parties accept, the server SHALL record an agreement capturing the accepted proposal version, both parties' identities, and the time of each acceptance.

The agreement record MUST be immutable, and both parties MUST be able to export it.

#### Scenario: Agreement is recorded on mutual acceptance

- **WHEN** both parties accept the same proposal version
- **THEN** an agreement is recorded referencing that exact version with both acceptance times

#### Scenario: Agreement is immutable

- **WHEN** any party attempts to alter a recorded agreement
- **THEN** the attempt is refused; changes require a new proposal and a new agreement

#### Scenario: Both parties can export

- **WHEN** either party exports the agreement
- **THEN** they receive its full content in a documented, machine-readable format

### Requirement: The platform is not a party

The server SHALL make clear that it records what the parties agreed and does not itself provide escrow, payment, guarantees, or dispute resolution unless an operator has explicitly enabled such an integration.

The platform MUST NOT present an agreement in a way that implies the operator warrants it.

#### Scenario: Role is disclosed

- **WHEN** an agreement is recorded
- **THEN** both parties are shown what the platform does and does not guarantee

#### Scenario: No implied warranty

- **WHEN** an agreement is displayed or exported
- **THEN** it does not represent the operator as a party to it or as guaranteeing performance

### Requirement: Linking to work

An engagement SHALL be linkable to a project, so that agreed work can be tracked as issues.

Linking MUST NOT expose the engagement's private discussion or commercial terms to project members who are not parties to it.

#### Scenario: Engagement links to a project

- **WHEN** an engagement is linked to a project
- **THEN** both parties can navigate between them

#### Scenario: Commercial terms stay private

- **WHEN** a project member who is not a party views the linked project
- **THEN** they see the work items without the engagement's proposals, prices, or private discussion

### Requirement: Completion and record

Either party SHALL be able to mark work complete, requiring confirmation from the other, and the completed engagement MUST remain as a record for both parties.

#### Scenario: Completion requires confirmation

- **WHEN** one party marks the work complete
- **THEN** the engagement awaits the other party's confirmation before being recorded as complete

#### Scenario: Record persists

- **WHEN** an engagement is complete
- **THEN** both parties retain access to its agreement and history

#### Scenario: Unconfirmed completion does not expire silently

- **WHEN** a completion request is not confirmed
- **THEN** the engagement remains in its current state and both parties can see that confirmation is outstanding
