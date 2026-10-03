## Purpose

Defines the tamper-evident record of security- and administration-relevant actions on a TDC server, so operators can answer who did what and when, and so moderation and administrative power can be reviewed rather than merely trusted.

## Requirements

### Requirement: Recorded actions

The server SHALL record in an audit log every authentication event, every authorization change, every moderator action, every administrative configuration change, and every access to the audit log itself.

Each entry MUST record the actor, the action, the affected target, the time, and the source of the request.

#### Scenario: Administrative change is recorded

- **WHEN** an administrator changes a server setting
- **THEN** an audit entry records the actor, the setting, its previous and new values, and the time

#### Scenario: Authentication events are recorded

- **WHEN** a sign-in succeeds or fails
- **THEN** an audit entry records the outcome, the account involved, and the request source

#### Scenario: Reading the audit log is itself audited

- **WHEN** a user reads the audit log
- **THEN** that access is recorded as an audit entry

### Requirement: Access to the audit log

Reading the audit log SHALL require a specific permission, distinct from general administrative permissions, so that audit access can be granted or withheld independently.

#### Scenario: Audit access is separately granted

- **WHEN** an administrator lacks the audit-read permission
- **THEN** they cannot read the audit log despite holding other administrative permissions

#### Scenario: Permitted reader can query

- **WHEN** a user with the audit-read permission queries the log by actor, target, action type, or time range
- **THEN** matching entries are returned

### Requirement: Integrity

Audit entries SHALL be append-only. The server MUST NOT provide any interface for editing or selectively deleting entries, and modification of stored entries MUST be detectable.

#### Scenario: No edit interface exists

- **WHEN** any user, including an administrator, attempts to alter or delete an individual audit entry through the application
- **THEN** no such capability is available

#### Scenario: Tampering is detectable

- **WHEN** a stored audit entry is altered outside the application
- **THEN** an integrity check over the log detects that alteration

### Requirement: Retention and export

Operators SHALL be able to configure how long audit entries are retained, and to export the log in a documented, machine-readable format.

Expiry of audit entries MUST itself be recorded, so that a gap in the log is explained rather than unexplained.

#### Scenario: Export produces a usable record

- **WHEN** an operator exports the audit log
- **THEN** the export is in a documented machine-readable format containing all fields of each entry

#### Scenario: Expiry is accounted for

- **WHEN** audit entries are removed because their retention period elapsed
- **THEN** an entry records what period was removed and when

### Requirement: Privacy in audit records

Audit entries SHALL record that an action occurred without embedding message content or credentials.

Where an entry concerns content, it MUST reference that content by identifier rather than copying it.

#### Scenario: No credentials in the log

- **WHEN** an authentication event is recorded
- **THEN** the entry contains no password, token, or other credential material

#### Scenario: Content is referenced, not copied

- **WHEN** a moderator removes a message and the action is audited
- **THEN** the entry identifies the message rather than reproducing its content

### Requirement: Availability

Failure to write an audit entry SHALL be treated as a failure of the audited action for security-relevant operations, so that a privileged action cannot proceed unrecorded.

#### Scenario: Unrecordable privileged action is refused

- **WHEN** an administrative or moderation action cannot be written to the audit log
- **THEN** the action is refused rather than performed without a record

#### Scenario: Ordinary messaging is unaffected

- **WHEN** the audit subsystem is degraded
- **THEN** ordinary messaging continues, since it is not an audited privileged action
