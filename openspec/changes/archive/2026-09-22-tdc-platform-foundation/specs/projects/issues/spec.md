## Purpose

Defines trackable units of work — issues with states, assignees, labels, relationships, and discussion — so that conversation in TDC can become accountable work without moving to a separate tool.

## ADDED Requirements

### Requirement: Issues

Users SHALL be able to create issues within a project, each having a title, an optional description, a state, and an author.

Every issue MUST have an identifier that is stable, human-readable, and usable to reference it from messages.

#### Scenario: Issue is created

- **WHEN** a user with permission creates an issue with a title
- **THEN** the issue exists in the project with a stable identifier and an initial state

#### Scenario: Issue is referenced from a message

- **WHEN** a user references an issue's identifier in a message
- **THEN** the message displays the issue's title and current state

#### Scenario: Identifier is stable

- **WHEN** an issue is renamed, moved between states, or reassigned
- **THEN** its identifier does not change

### Requirement: States and workflow

Each project SHALL define the states an issue can occupy, and operators or project administrators MUST be able to configure them, including which represent completion.

Transitions MUST be recorded with the actor and time, and moving an issue to a state that does not exist MUST be refused.

#### Scenario: State transition is recorded

- **WHEN** a user moves an issue to a different state
- **THEN** the issue reflects the new state
- **AND** the transition is recorded with the actor and time

#### Scenario: Completion is derivable

- **WHEN** a project's states are configured
- **THEN** it is unambiguous which states mean the work is complete

#### Scenario: Removing a state in use is handled

- **WHEN** a state currently holding issues is removed
- **THEN** the operation either is refused or requires the issues to be moved, rather than leaving them in a nonexistent state

### Requirement: Assignment

Issues SHALL be assignable to one or more users, and a user MUST be able to see the issues assigned to them across projects.

Assigning a user MUST notify them according to their notification preferences.

#### Scenario: Assignment notifies

- **WHEN** a user is assigned an issue
- **THEN** they are notified according to their preferences

#### Scenario: Cross-project view

- **WHEN** a user views their assigned work
- **THEN** issues assigned to them across all projects they can access are listed

#### Scenario: Unassignment is recorded

- **WHEN** an assignee is removed from an issue
- **THEN** the change is recorded with the actor and time

### Requirement: Labels and priority

Issues SHALL support labels and a priority, both configurable per project, so work can be categorised and ordered.

#### Scenario: Issues are filterable by label

- **WHEN** a user filters a project's issues by a label
- **THEN** only issues carrying that label are returned

#### Scenario: Priority orders work

- **WHEN** issues are sorted by priority
- **THEN** they are returned in the configured priority order

### Requirement: Relationships

Issues SHALL support relationships to other issues, including parent-child and blocking relationships.

The system MUST prevent a relationship that would create a cycle.

#### Scenario: Blocking relationship is visible

- **WHEN** one issue blocks another
- **THEN** both issues display the relationship

#### Scenario: Cycle is refused

- **WHEN** a relationship would create a cycle among issues
- **THEN** the relationship is refused with an explanation

#### Scenario: Parent reflects children

- **WHEN** an issue has child issues
- **THEN** the parent shows the completion state of its children

### Requirement: Discussion

Each issue SHALL have a discussion where users can comment, with the same formatting, attachment, and mention behaviour as channel messages.

Mentioning a user in an issue MUST notify them.

#### Scenario: Comment notifies participants

- **WHEN** a user comments on an issue
- **THEN** its assignees and previous participants are notified according to their preferences

#### Scenario: Attachments behave consistently

- **WHEN** a user attaches a file to an issue comment
- **THEN** it is subject to the same limits, checks, and access control as a channel attachment

### Requirement: Linking conversation to work

Users SHALL be able to create an issue from a message, and the resulting issue MUST retain a reference back to that message.

#### Scenario: Issue created from a message

- **WHEN** a user creates an issue from a message
- **THEN** the issue is created with a reference to the originating message and channel
- **AND** the message indicates that an issue was created from it

#### Scenario: Reference respects permissions

- **WHEN** a user without access to the originating channel views the issue
- **THEN** the reference does not disclose the message content

### Requirement: History

Every change to an issue SHALL be recorded, showing what changed, who changed it, and when.

History MUST NOT be editable.

#### Scenario: Change history is visible

- **WHEN** a user views an issue's history
- **THEN** each change is listed with the field, its previous and new values, the actor, and the time

#### Scenario: History cannot be rewritten

- **WHEN** any user attempts to alter a recorded history entry
- **THEN** no such capability exists

### Requirement: Search and filtering

Users SHALL be able to search and filter issues by text, state, assignee, label, priority, and project, and results MUST exclude issues in projects the user cannot access.

#### Scenario: Search respects access

- **WHEN** a user searches issues
- **THEN** results include only issues from projects they can access

#### Scenario: Filters combine

- **WHEN** a user applies several filters together
- **THEN** results satisfy all of them
