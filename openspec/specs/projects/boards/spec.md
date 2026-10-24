## Purpose

Defines the ways a team sees and plans its work — projects, boards, saved views, and time-boxed cycles — so that TDC members and their clients can agree on what is being done now and what comes next.

## Requirements

### Requirement: Projects

Issues SHALL be organised into projects, each with a name, a key used in issue identifiers, and its own membership and permissions.

A project's visibility MUST be controllable so that client-facing work can be separated from internal work.

#### Scenario: Project scopes its issues

- **WHEN** a user views a project
- **THEN** they see the issues belonging to it, subject to their permissions

#### Scenario: Private project is not disclosed

- **WHEN** a user without access queries or searches
- **THEN** the private project and its issues do not appear in any result

#### Scenario: Project key appears in identifiers

- **WHEN** an issue is created in a project
- **THEN** its identifier incorporates the project's key

### Requirement: A project's history is complete

Every change to a project's rows — its configuration, membership, planning and issues — SHALL be recorded as a signed event in the project's stream, in the same transaction as the change.

Replaying the stream alone in order MUST arrive at exactly the rows the project holds. A project whose history predates this SHALL be given one baseline event recording its rows.

#### Scenario: The stream reproduces the project

- **WHEN** a project's stream is replayed into an empty database
- **THEN** its board, lists and progress figures read exactly as the original's

#### Scenario: An older project becomes complete

- **WHEN** a server starts with a project made before every change was recorded
- **THEN** it appends one baseline of that project's rows, and a replay from it reproduces the project

### Requirement: A project's own history is shown

What happens to a project itself — rather than to one of its issues — SHALL be shown as the project's history, newest first, each entry saying who did what and when. Like an issue's history it MUST NOT be editable.

#### Scenario: A configuration change is recorded

- **WHEN** somebody adds, renames or removes a label or a priority level
- **THEN** the project's history shows it with who did it and when

#### Scenario: History cannot be rewritten

- **WHEN** anybody attempts to change or remove an entry in a project's history
- **THEN** no such capability exists

### Requirement: Boards

Projects SHALL offer a board view presenting issues in columns derived from their states, where an issue can be moved between columns.

Moving an issue on a board MUST change its state and be recorded like any other state transition.

#### Scenario: Board reflects states

- **WHEN** a user opens a project's board
- **THEN** issues appear in columns corresponding to their states

#### Scenario: Moving on the board changes state

- **WHEN** a user moves an issue to another column
- **THEN** the issue's state changes accordingly and the transition is recorded

#### Scenario: Concurrent moves converge

- **WHEN** two users move the same issue at nearly the same time
- **THEN** the result is deterministic and both users converge on the same final state

### Requirement: Views

Users SHALL be able to view issues as a list or a board, to sort and filter them, and to save a configured view for reuse.

A saved view MUST be shareable with others, who see it subject to their own permissions.

#### Scenario: View is saved and reused

- **WHEN** a user saves a filtered, sorted view
- **THEN** they can return to it later and see it applied

#### Scenario: Shared view respects the viewer's permissions

- **WHEN** a shared view is opened by another user
- **THEN** it shows only issues that user is permitted to see

### Requirement: Cycles

Projects SHALL optionally support time-boxed cycles with a start and end date, to which issues can be assigned.

At a cycle's end, incomplete issues MUST NOT be silently discarded; the system MUST either carry them forward or leave them assigned, according to configuration.

#### Scenario: Issues are assigned to a cycle

- **WHEN** a user assigns issues to a cycle
- **THEN** the cycle shows those issues and their states

#### Scenario: Incomplete work is not lost

- **WHEN** a cycle ends with incomplete issues
- **THEN** those issues are carried forward or remain assigned per configuration, and are never silently removed

#### Scenario: Cycles are optional

- **WHEN** a project does not use cycles
- **THEN** all issue and board functionality remains available

### Requirement: Progress reporting

Projects and cycles SHALL report progress, showing how many issues are complete against the total, and the trend over the period.

Reported figures MUST be derived from recorded state transitions rather than maintained as independent counters that can drift.

#### Scenario: Progress is shown

- **WHEN** a user views a project or cycle
- **THEN** completed and remaining work is shown

#### Scenario: Figures are derived, not drifting

- **WHEN** progress is recomputed from the recorded history
- **THEN** it matches the reported figures

#### Scenario: Charts degrade gracefully

- **WHEN** a visualisation cannot be rendered
- **THEN** the underlying figures remain available in a readable form

### Requirement: Milestones

Projects SHALL support grouping issues under a named milestone with an optional target date, spanning cycles.

#### Scenario: Milestone groups work

- **WHEN** issues are assigned to a milestone
- **THEN** the milestone reports its issues and their completion

#### Scenario: Target date is advisory

- **WHEN** a milestone's target date passes with incomplete issues
- **THEN** the milestone is shown as overdue without any issue being altered automatically

### Requirement: Bulk operations

Users SHALL be able to act on several issues at once, including changing state, assignee, labels, and cycle.

A bulk operation MUST report which items succeeded and which failed, and MUST NOT leave a partially applied change unreported.

#### Scenario: Bulk change reports outcomes

- **WHEN** a user applies a change to several issues and some fail on permissions
- **THEN** the successful changes are applied
- **AND** the user is told exactly which failed and why

#### Scenario: Bulk operations are recorded

- **WHEN** a bulk operation completes
- **THEN** each affected issue records its own change in history
