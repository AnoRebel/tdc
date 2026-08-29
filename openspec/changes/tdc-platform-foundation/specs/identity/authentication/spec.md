## Purpose

Defines how a person proves who they are to a TDC server, covering email and password, OAuth2 providers, and passkeys, together with the session, recovery, and account-security guarantees that protect those identities.

## ADDED Requirements

### Requirement: Supported authentication methods

The server SHALL support email-and-password, OAuth2 with an external provider, and passkey (WebAuthn) authentication. Each method MUST be independently enableable by the operator, and the server MUST refuse to start with every method disabled.

No authentication method may require a paid service; operators MUST be able to run a server offering all three without purchasing anything.

#### Scenario: Operator restricts methods

- **WHEN** an operator enables only passkeys and OAuth2
- **THEN** the sign-in interface offers only those methods
- **AND** attempts to authenticate with a password are rejected

#### Scenario: All methods disabled is refused

- **WHEN** the server starts with every authentication method disabled
- **THEN** it exits with an explanation rather than serving an unreachable instance

### Requirement: Password authentication

When password authentication is enabled, passwords SHALL be stored using a memory-hard password hashing function with per-password salts, and MUST never be stored or logged in recoverable form.

The server MUST reject passwords known to be compromised or trivially weak, and MUST NOT impose composition rules that force predictable substitutions.

#### Scenario: Password is never recoverable

- **WHEN** a password is set
- **THEN** only a salted hash is persisted
- **AND** no log, audit record, or error message contains the password

#### Scenario: Weak password is rejected at set time

- **WHEN** a user sets a password that is trivially weak or known to be compromised
- **THEN** the change is rejected with an explanation of why

#### Scenario: Failure response does not disclose account existence

- **WHEN** authentication fails
- **THEN** the response does not reveal whether the account exists

### Requirement: Passkey authentication

The server SHALL support registering and authenticating with passkeys per WebAuthn, allowing a user to enrol more than one credential and to name, list, and revoke them.

A user MUST NOT be able to remove their last remaining authentication method.

#### Scenario: Multiple passkeys per user

- **WHEN** a user enrols a second passkey
- **THEN** either passkey can be used to sign in
- **AND** both appear in the user's credential list with their names and enrolment dates

#### Scenario: Last credential cannot be removed

- **WHEN** a user attempts to remove their only remaining means of authentication
- **THEN** the removal is refused with an explanation

#### Scenario: Revoked passkey stops working

- **WHEN** a user revokes a passkey
- **THEN** subsequent authentication attempts with that credential fail

### Requirement: OAuth2 authentication

The server SHALL support authenticating through OAuth2 providers configured by the operator, including self-hostable providers, so that no external commercial account is required.

An OAuth2 identity MUST be linkable to an existing account rather than silently creating a duplicate when the verified email matches.

#### Scenario: Self-hosted provider works

- **WHEN** an operator configures a self-hosted OAuth2 provider
- **THEN** users can sign in through it without any commercial provider being configured

#### Scenario: Matching verified email links rather than duplicates

- **WHEN** a user signs in via OAuth2 with a verified email matching an existing account
- **THEN** the user is prompted to link the identity to that account rather than a second account being created

#### Scenario: Unverified email does not auto-link

- **WHEN** an OAuth2 provider asserts an email it has not verified
- **THEN** the server does not automatically link it to an existing account

### Requirement: Sessions

Authenticating SHALL establish a session bound to the authenticating device. Session credentials MUST expire, MUST be revocable individually and in bulk, and MUST be invalidated when the user's password is changed or a credential is revoked.

Users MUST be able to see their active sessions with enough context to recognise them, and end any of them.

#### Scenario: User ends a remote session

- **WHEN** a user ends one of their active sessions
- **THEN** requests using that session's credentials are rejected immediately

#### Scenario: Password change invalidates other sessions

- **WHEN** a user changes their password
- **THEN** all other sessions for that user are invalidated

#### Scenario: Expired session is rejected

- **WHEN** a session credential is used after expiry
- **THEN** the request is rejected and the client is directed to re-authenticate

### Requirement: Account recovery

The server SHALL provide account recovery that does not depend on a paid service. Recovery tokens MUST be single-use, time-limited, and invalidated once used or once a newer token is issued.

Recovery MUST NOT reveal whether an account exists for a given address.

#### Scenario: Recovery token is single-use

- **WHEN** a recovery token is used once
- **THEN** a second use of the same token is rejected

#### Scenario: Recovery does not disclose account existence

- **WHEN** recovery is requested for an address with no account
- **THEN** the response is indistinguishable from a request for an address with an account

#### Scenario: Recovery works without a paid provider

- **WHEN** an operator configures recovery using a self-hosted mail server
- **THEN** recovery functions with no commercial service configured

### Requirement: Brute-force and abuse resistance

Authentication attempts SHALL be rate-limited per account and per source, with progressive delay or lockout after repeated failures. Rate limiting MUST NOT be circumventable by varying the case or formatting of an identifier.

#### Scenario: Repeated failures are throttled

- **WHEN** repeated failed authentication attempts are made against one account
- **THEN** further attempts are progressively delayed or refused
- **AND** the events are recorded in the audit log

#### Scenario: Identifier normalisation is applied

- **WHEN** attempts vary only by letter case or equivalent formatting of the identifier
- **THEN** they count toward the same rate limit

### Requirement: First-run setup

On first start with no accounts, the server SHALL provide a bounded process for creating the initial administrator, and that process MUST become unavailable once an administrator exists.

The server MUST NOT ship with any default or well-known credential.

#### Scenario: First administrator is created once

- **WHEN** the first administrator account is created
- **THEN** the first-run setup path stops being available

#### Scenario: No default credentials exist

- **WHEN** a newly installed server is started
- **THEN** no account exists that can be accessed with a credential published in the documentation or source
