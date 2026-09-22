## Purpose

Defines what an operator can configure about their own TDC server — limits, language, branding, registration policy, and which capabilities exist at all — so that independently hosted servers can serve different communities on their own terms.

## ADDED Requirements

### Requirement: Server identity

Each server SHALL have a name, a description, and a canonical address by which it is known, and MUST expose these so that clients and other servers can identify it.

The canonical address MUST be settable by the operator and MUST NOT be inferred from an incoming request's headers.

#### Scenario: Server identifies itself

- **WHEN** a client or peer requests the server's public information
- **THEN** the server returns its name, description, canonical address, and version

#### Scenario: Address is not header-derived

- **WHEN** a request arrives carrying a host header different from the configured canonical address
- **THEN** the server continues to identify itself by its configured address

### Requirement: Capacity limits

Operators SHALL be able to limit the number of registered users, the number of channels, per-user storage, and total storage.

When a limit is reached, the server MUST refuse the action that would exceed it with an explanatory message, and MUST NOT partially apply it.

#### Scenario: User limit is enforced

- **WHEN** the configured maximum number of users is reached and another registration is attempted
- **THEN** the registration is refused with an explanation

#### Scenario: Storage limit is enforced

- **WHEN** an upload would exceed the configured storage limit
- **THEN** the upload is refused and no partial file is retained

#### Scenario: Operators can see usage against limits

- **WHEN** an administrator views server status
- **THEN** current usage is shown against each configured limit

### Requirement: Registration policy

Operators SHALL be able to choose whether registration is open, invitation-only, or closed, and to restrict registration to particular email domains.

Changing the policy MUST NOT affect existing accounts.

#### Scenario: Invitation-only registration

- **WHEN** registration is invitation-only and someone attempts to register without a valid invitation
- **THEN** the registration is refused

#### Scenario: Domain restriction

- **WHEN** registration is restricted to particular email domains and an address outside them is used
- **THEN** the registration is refused

#### Scenario: Existing accounts are unaffected

- **WHEN** an operator closes registration
- **THEN** existing users continue to sign in normally

### Requirement: Language

Operators SHALL be able to set the server's default language, which applies to new users and to server-generated messages.

Users MUST retain the ability to choose a different language for themselves.

#### Scenario: Default applies to new users

- **WHEN** an operator sets the default language and a new user joins
- **THEN** that user's interface begins in the default language

#### Scenario: User choice wins

- **WHEN** a user selects a different language
- **THEN** their choice is used regardless of the server default

### Requirement: Branding

Operators SHALL be able to set the server's display name, logo, and accent colour without modifying application source.

Branding MUST NOT be able to alter or hide security-relevant information such as the server address a client is connected to.

#### Scenario: Branding applies without a rebuild

- **WHEN** an operator sets a logo and accent colour
- **THEN** clients display them without the application being rebuilt from modified source

#### Scenario: Security information cannot be hidden

- **WHEN** any branding is configured
- **THEN** the connected server's address remains discoverable by the user

### Requirement: Capability toggles

Operators SHALL be able to enable or disable optional capabilities individually, and clients MUST adapt to the advertised set rather than assuming any capability exists.

Disabling a capability MUST NOT delete data belonging to it, so the change is reversible.

#### Scenario: Client adapts to advertised capabilities

- **WHEN** a client connects to a server with a capability disabled
- **THEN** the client does not present that capability

#### Scenario: Disabling preserves data

- **WHEN** a capability is disabled and later re-enabled
- **THEN** the data it held is intact

### Requirement: Configuration changes are auditable and safe

Changing server configuration SHALL be recorded in the audit log with the actor and the previous and new values.

A change that would immediately break the operator's own access MUST be refused or require explicit confirmation.

#### Scenario: Configuration change is recorded

- **WHEN** an administrator changes a server setting
- **THEN** the audit log records the actor, setting, and both values

#### Scenario: Self-lockout is prevented

- **WHEN** a configuration change would immediately remove the acting administrator's ability to administer the server
- **THEN** the change is refused or requires explicit confirmation

### Requirement: Configuration is inspectable

Administrators SHALL be able to view the effective configuration, including which values came from defaults, file, or environment.

Secret values MUST be redacted when displayed.

#### Scenario: Effective configuration is visible

- **WHEN** an administrator views the server configuration
- **THEN** each value is shown with its source

#### Scenario: Secrets are redacted

- **WHEN** configuration containing secrets is displayed or exported
- **THEN** secret values are redacted rather than shown
