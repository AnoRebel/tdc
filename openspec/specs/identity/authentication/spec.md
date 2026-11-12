## Purpose

Defines how a person proves who they are to a TDC server, covering email and password, OAuth2 providers, and passkeys, together with the session, recovery, and account-security guarantees that protect those identities.

## Requirements

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

### Requirement: Multi-factor authentication

The server SHALL support an optional second factor on an account that signs in with a password. A user MUST be able to enable it for themselves, and an operator MUST be able to require it of everybody or of administrators only.

Supported factors SHALL include time-based one-time codes (TOTP), a passkey acting as a second factor, and single-use recovery codes. An emailed code MAY be offered where the operator has configured mail.

Recovery codes MUST be issued when a second factor is first enabled, MUST be single-use, and MUST be stored so that a database read does not yield working ones.

No second factor may require a paid service.

A second factor MUST NOT be requested where the first factor already proves possession of a device — signing in with a passkey alone SHALL NOT then ask for a second factor.

#### Scenario: Somebody enables a second factor

- **WHEN** a user enables TOTP
- **THEN** subsequent sign-ins with their password require a current code
- **AND** they are given recovery codes at the moment it is enabled

#### Scenario: Recovery code is single-use

- **WHEN** a user signs in with a recovery code
- **THEN** that code no longer works
- **AND** the remaining codes are unaffected

#### Scenario: Operator requires a second factor

- **WHEN** an operator requires MFA of everybody
- **THEN** a user without one is asked to enrol before they can use the server
- **AND** an existing session does not bypass the requirement

#### Scenario: A passkey is already a second factor

- **WHEN** a user with MFA enabled signs in with a passkey
- **THEN** no further factor is requested

#### Scenario: Wrong code does not disclose more than a wrong password

- **WHEN** a second factor is entered incorrectly
- **THEN** the refusal does not say whether the first factor was right

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

### Requirement: Account names

A person SHALL be able to change their own display name and, under the operator's rules, their own username.

The display name MUST be freely changeable by its owner, subject to the same rules registration applies: at most 64 characters once trimmed, no control characters, no invisible formatting characters except the joiner inside an emoji sequence, and not a reserved name however it is punctuated. An empty display name MUST mean the username is shown.

Because the username is half of the person's address, a username change:

- MUST be allowed only while the operator permits changes (on by default), and no sooner than the operator's interval after the person's previous change (30 days by default);
- MUST be confirmed by the account's current password, or, for an account without one, by a session begun within the last 15 minutes, and MUST be refused to API tokens;
- MUST be rate limited per account, counting failed confirmations;
- MUST NOT take a name another account holds, a reserved name, or a name another account gave up within the operator's holding period (90 days by default) — and registration MUST refuse a held name in the same way;
- MUST keep the old name resolving to the person wherever a username is looked up — profile links, saved filters, signing in — until another account takes it;
- MUST be refused while the person is a member of any room another server takes part in, because peers know people by address and the federation protocol cannot carry a rename;
- MUST be recorded in the audit log with both names, and administrators MUST be able to see the names an account held before.

Messages already sent MUST keep the address they were signed under, and the client MUST still treat them as the person's own.

#### Scenario: Display name changes at once

- **WHEN** a person sets a new display name
- **THEN** it is shown wherever their display name is shown from the next fetch

#### Scenario: Deceptive display name is refused

- **WHEN** a person sets a display name containing a right-to-left override, or spelling a reserved name
- **THEN** it is refused with the reason, at registration as at a change

#### Scenario: Username change needs proof

- **WHEN** a person asks to change their username without their current password, or with an API token
- **THEN** it is refused and nothing changes

#### Scenario: Old name is held for its owner

- **WHEN** a person changes their username
- **THEN** nobody else may register or take the old name during the holding period
- **AND** links to the old name resolve to the person

#### Scenario: Changes are paced

- **WHEN** a person changes their username again before the interval has passed
- **THEN** it is refused with the date it becomes possible

#### Scenario: Federated membership blocks a rename

- **WHEN** a person in a room shared with another server asks to change their username
- **THEN** it is refused with an explanation of why and what to do

#### Scenario: Renames are on the record

- **WHEN** a username changes
- **THEN** the audit log records the previous and new names
- **AND** administrators see the former name beside the account
