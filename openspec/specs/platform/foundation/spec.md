## Purpose

Defines the repository, build, release, packaging, configuration and documentation guarantees that every other TDC capability depends on, so that any person can obtain, verify, run, and contribute to a TDC server without paid tooling or undocumented steps.

## ADDED Requirements

### Requirement: Zero-cost self-hosting

Running a complete TDC server SHALL NOT require any paid licence, paid subscription, or third-party API key. Every dependency required to start a server and use its core features MUST be free, open-source, and self-hostable.

Features that cannot function without an external paid or key-bearing service MUST be optional, disabled by default, and individually toggleable. Such a feature MUST be documented with the key it requires, the cost model of the provider, and either a free or self-hostable alternative or an explicit statement that the feature is unavailable without it.

#### Scenario: Default deployment requires no keys

- **WHEN** an operator starts a TDC server using only the documented defaults and a Postgres connection string
- **THEN** the server starts successfully and all core capabilities (authentication, channels, messaging, attachments, notifications, moderation, audit) are usable
- **AND** no configuration value naming an external paid service is required

#### Scenario: Optional paid integration is disabled by default

- **WHEN** the server starts without configuration for an integration that requires an external API key
- **THEN** the server starts successfully
- **AND** the dependent feature reports itself as unavailable rather than failing the server or erroring at point of use

#### Scenario: Dependency licence regression fails the build

- **WHEN** a dependency is added whose licence is not on the project's approved-licence list
- **THEN** continuous integration fails with the offending dependency and its licence named

### Requirement: Licensing

The project SHALL be licensed under Apache-2.0, with the licence text at the repository root and per-file attribution as required by the project's contribution rules.

Because TDC's other repositories use GPL-3.0, the project MUST NOT incorporate code from those repositories or any other copyleft source without explicit relicensing by the copyright holder.

#### Scenario: Licence is discoverable

- **WHEN** a person obtains the repository
- **THEN** an Apache-2.0 `LICENSE` file is present at the root
- **AND** the README states the licence and the incompatibility with copying GPL-licensed TDC code

#### Scenario: Contribution of incompatible code is rejected

- **WHEN** a contribution is submitted that contains code under a licence incompatible with Apache-2.0 redistribution
- **THEN** the contribution is rejected with the incompatible source identified

### Requirement: Monorepo structure

The repository SHALL contain the server, the shared frontend packages, the web application, and the desktop application in a single versioned monorepo, using Bun workspaces for JavaScript/TypeScript packages and Go modules for Go packages.

Shared user-interface code MUST live in a package consumed by both the web and desktop applications, so that a change to shared behaviour reaches both clients without duplication.

#### Scenario: Single checkout builds everything

- **WHEN** a contributor clones the repository and runs the documented bootstrap command
- **THEN** all workspace dependencies install
- **AND** the server, web application, and desktop application can each be built without additional repository checkouts

#### Scenario: Shared UI change reaches both clients

- **WHEN** a component in the shared UI package is modified
- **THEN** both the web application and the desktop application build against the modified component without per-client duplication of that change

### Requirement: Versioning

The project SHALL use Semantic Versioning. All released artifacts produced from a single commit — server binary, container image, web bundle, and desktop installers — MUST report the same version string.

The server MUST expose its version, and the protocol version it speaks, over an unauthenticated endpoint so operators and federated peers can determine compatibility.

#### Scenario: Artifacts agree on version

- **WHEN** a release is produced from a tagged commit
- **THEN** every published artifact reports that same version

#### Scenario: Version is queryable

- **WHEN** any client or peer requests the server's version endpoint without credentials
- **THEN** the server returns its release version and supported protocol version(s)

### Requirement: Continuous integration

Every change proposed to the default branch SHALL be validated automatically before merge. Validation MUST include: build of every workspace, unit tests, linting and formatting checks, type checking, dependency licence checks, dependency vulnerability scanning, and database migration checks.

A change that fails any of these MUST NOT be mergeable.

#### Scenario: Failing check blocks merge

- **WHEN** a proposed change fails any required check
- **THEN** the change is reported as failing and cannot be merged

#### Scenario: Migrations are validated

- **WHEN** a change adds or alters a database migration
- **THEN** continuous integration applies the migration to a database at the previous release's schema and reports failure if it does not apply cleanly

### Requirement: Releases

Tagging a release SHALL automatically produce and publish: server binaries for Linux, macOS, and Windows on amd64 and arm64; a multi-architecture container image; and desktop application installers for Linux, macOS, and Windows.

Every published artifact MUST be accompanied by a checksum, and release notes MUST be generated from the changes included since the previous release.

#### Scenario: Tag produces complete release

- **WHEN** a maintainer pushes a release tag
- **THEN** binaries, container images, and desktop installers for all supported platforms are published
- **AND** each carries a verifiable checksum
- **AND** release notes listing the included changes are attached

#### Scenario: Release failure is visible

- **WHEN** any artifact in a release fails to build or publish
- **THEN** the release is reported as failed rather than published partially and silently

### Requirement: Container packaging

The project SHALL provide a container image for the server and a Compose definition that brings up a working deployment consisting of the server and Postgres.

The Compose definition MUST run without editing for local evaluation, and MUST document every value an operator is expected to change before production use.

#### Scenario: Compose brings up a usable server

- **WHEN** an operator runs the provided Compose definition on a machine with a container runtime
- **THEN** a TDC server and its database start
- **AND** the operator can complete first-run setup and sign in through the web application

#### Scenario: Production-unsafe defaults are flagged

- **WHEN** the server starts with a default credential or secret that is unsafe for production
- **THEN** the server logs a prominent warning naming each such value

### Requirement: Configuration

Server configuration SHALL be supplied by file and overridable by environment variable, with environment taking precedence. All configuration MUST be documented, including type, default, and effect.

The server MUST validate configuration at startup and refuse to start on invalid configuration, reporting every problem found rather than only the first.

#### Scenario: Invalid configuration is rejected clearly

- **WHEN** the server starts with two invalid configuration values
- **THEN** the server exits without serving traffic
- **AND** reports both invalid values with the reason for each

#### Scenario: Environment overrides file

- **WHEN** a configuration value is set in both the file and the environment
- **THEN** the environment value takes effect

### Requirement: Feature toggles

Every optional capability SHALL be independently toggleable by the operator, with toggles evaluated at runtime.

When a capability is disabled, its user interface MUST NOT be presented to users, and its endpoints MUST reject requests rather than partially serving them.

#### Scenario: Disabled capability is fully absent

- **WHEN** an operator disables an optional capability
- **THEN** clients do not present entry points to that capability
- **AND** requests to its endpoints are rejected with a clear "not enabled" response

#### Scenario: Clients discover enabled capabilities

- **WHEN** a client connects to a server
- **THEN** the server advertises which optional capabilities are enabled, so the client can adapt its interface without hardcoding assumptions

### Requirement: Documentation

The repository SHALL contain and keep current: a README, a hosting/operations guide, a user guide, a contribution guide, an integration guide, an upgrade/migration guide, and a security policy.

Documentation MUST be updated in the same change that alters the behaviour it describes.

#### Scenario: Behaviour change without documentation is rejected

- **WHEN** a change alters user-visible behaviour, a configuration value, or an API contract without updating the corresponding documentation
- **THEN** the change is rejected in review as incomplete

#### Scenario: Hosting guide is sufficient to deploy

- **WHEN** an operator with general Linux administration experience and no prior TDC knowledge follows the hosting guide
- **THEN** they can deploy, secure with TLS, back up, and upgrade a TDC server without consulting the source code

### Requirement: Upgrades and data safety

The server SHALL apply database migrations automatically on start, within a transaction where the database supports it, and MUST refuse to start against a database whose schema is newer than the running binary.

Upgrading across one or more intermediate releases MUST be supported without manual intervention.

#### Scenario: Skipped releases upgrade cleanly

- **WHEN** an operator upgrades a server across several intervening releases at once
- **THEN** all pending migrations apply in order and the server starts

#### Scenario: Downgrade is refused, not attempted

- **WHEN** a server binary starts against a database migrated by a newer release
- **THEN** the server exits with an explanation rather than modifying the schema

#### Scenario: Failed migration does not corrupt

- **WHEN** a migration fails partway
- **THEN** the schema is left at its prior state and the server does not serve traffic

### Requirement: Resource efficiency

A TDC server serving a small community SHALL run within resources an operator can obtain cheaply. An idle single-server deployment supporting up to 50 active users SHOULD operate within 1 GB of RAM for the server process, excluding the database.

This figure is a design target rather than an absolute limit: where meeting it would require sacrificing correctness, security, or maintainability, the better-engineered choice SHALL be taken and its cost recorded. Resource use MUST be measured in continuous integration and reported on every change, and exceeding the target MUST be surfaced and justified rather than passing unnoticed.

#### Scenario: Small deployment fits a small machine

- **WHEN** a server with up to 50 active users runs a representative workload
- **THEN** the server process's memory use is measured and reported against the 1 GB target

#### Scenario: Resource change is always visible

- **WHEN** a change increases idle memory or startup time
- **THEN** continuous integration reports the measured figures against the target and the change from the previous baseline

#### Scenario: Exceeding the target requires justification

- **WHEN** a change takes the server beyond the documented target
- **THEN** the excess is surfaced in review and the reason is recorded, rather than the change being blocked automatically or passing silently
