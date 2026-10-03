## Purpose

Defines the TDC desktop application for Linux, macOS, and Windows — what it must do beyond the web application to justify existing as a native client, and how it stays behaviourally consistent with the web while doing so.

## Requirements

### Requirement: Supported platforms

The desktop application SHALL be built and released for Linux, macOS, and Windows on 64-bit Intel and ARM architectures, produced from the same commit as every other release artifact.

Each platform's build MUST be verified in continuous integration, and a platform that fails to build MUST fail the release rather than being published incomplete.

#### Scenario: All platforms are produced

- **WHEN** a release is tagged
- **THEN** installers are published for each supported platform and architecture

#### Scenario: Broken platform fails the release

- **WHEN** the build for one platform fails
- **THEN** the release fails rather than publishing the remaining platforms silently

### Requirement: Shared behaviour with the web application

The desktop application SHALL present the same capabilities and behaviour as the web application, differing only where a native capability adds something the browser cannot provide.

Shared interface code MUST be consumed from a common package rather than duplicated, so a change to shared behaviour reaches both clients.

#### Scenario: Feature parity

- **WHEN** a capability is available in the web application
- **THEN** it is available in the desktop application unless documented as web-only

#### Scenario: Shared change reaches both

- **WHEN** shared interface code is changed
- **THEN** both clients reflect the change without separate reimplementation

#### Scenario: Transport differs without behaviour differing

- **WHEN** the desktop client communicates with the server by a different mechanism than the web client
- **THEN** the observable behaviour of shared features is identical

### Requirement: Native notifications

The desktop application SHALL raise native operating system notifications for events the user has chosen to be notified about, including while the window is not focused.

Notifications MUST respect the operating system's quiet or focus modes, and activating one MUST bring the user to the relevant conversation.

#### Scenario: Background notification

- **WHEN** a notifiable event occurs while the application is not focused
- **THEN** a native notification is raised

#### Scenario: Activation navigates

- **WHEN** a user activates a notification
- **THEN** the application is brought forward showing the relevant conversation

### Requirement: System tray

The desktop application SHALL provide a system tray presence showing whether there is unread activity, and MUST be able to run with its window closed while remaining reachable from the tray.

The user MUST be able to disable tray behaviour and closing-to-tray.

#### Scenario: Unread activity is visible in the tray

- **WHEN** unread activity exists while the window is closed
- **THEN** the tray indicates it

#### Scenario: Close-to-tray is configurable

- **WHEN** a user disables closing to the tray
- **THEN** closing the window exits the application

### Requirement: Secure credential storage

The desktop application SHALL store long-lived credentials in the operating system's secure credential store where one is available.

Where no secure store is available, the application MUST NOT silently fall back to plaintext storage; it MUST either protect the credential with a user-supplied secret or require re-authentication each launch, and MUST tell the user which applies.

#### Scenario: Credentials use the OS store

- **WHEN** the operating system provides a secure credential store
- **THEN** long-lived credentials are stored there rather than in plain files

#### Scenario: Absent store does not degrade silently

- **WHEN** no secure credential store is available
- **THEN** the application informs the user and applies a documented alternative rather than writing the credential in plaintext

### Requirement: Launch and window behaviour

The desktop application SHALL optionally start when the user logs in, and MUST restore its previous window size and position on launch.

Starting at login MUST be disabled by default.

#### Scenario: Autostart is opt-in

- **WHEN** the application is installed
- **THEN** it does not start at login until the user enables it

#### Scenario: Window state is restored

- **WHEN** the application is launched after being closed
- **THEN** it restores its previous size and position, adjusted if that position is no longer on a connected display

### Requirement: Updates

The desktop application SHALL be able to check for updates and inform the user when one is available.

An update MUST be verified as authentic before installation, and verification failure MUST abort the update rather than proceeding.

#### Scenario: User is told about updates

- **WHEN** a newer release is available
- **THEN** the user is informed and can choose to update

#### Scenario: Unverified update is refused

- **WHEN** a downloaded update fails authenticity verification
- **THEN** it is discarded and not installed
- **AND** the user is informed

#### Scenario: Update checking is optional

- **WHEN** an operator or user disables update checks
- **THEN** no update requests are made

### Requirement: Connecting to a chosen server

The desktop application SHALL allow the user to enter the address of the TDC server they wish to use, since servers are independently hosted.

The application MUST NOT be built with a single hard-coded server, and MUST refuse to connect over an unencrypted connection except to a loopback address.

#### Scenario: User chooses their server

- **WHEN** a user first launches the application
- **THEN** they are asked which server to connect to

#### Scenario: Insecure connection is refused

- **WHEN** a user enters a server address that is not encrypted and is not a loopback address
- **THEN** the application refuses to connect and explains why

#### Scenario: Multiple servers

- **WHEN** a user has accounts on more than one server
- **THEN** they can switch between them without reinstalling or losing the other's session

### Requirement: Resource use

The desktop application SHALL remain responsive and modest in resource use while idle, staying within a documented memory budget verified during release builds.

#### Scenario: Idle application is modest

- **WHEN** the application is running, connected, and idle
- **THEN** its memory use remains within the documented budget

#### Scenario: Regression is caught

- **WHEN** a change increases idle resource use beyond the documented budget
- **THEN** the build reports a failure naming the measured and budgeted figures
