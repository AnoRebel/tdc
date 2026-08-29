## Purpose

Defines the TDC web application — an installable, offline-tolerant progressive web app built for mid-range phones on metered mobile data, since that is how most TDC members will reach the platform.

## ADDED Requirements

### Requirement: Installable progressive web app

The web application SHALL be installable to a device home screen, presenting an application manifest with icons, name, theme, and standalone display.

Installation MUST be offered without obstructing users who do not want it, and the offer MUST be dismissible and not repeated indefinitely.

#### Scenario: Application is installable

- **WHEN** a user visits the application in a browser supporting installation
- **THEN** the application is installable to the home screen and launches in standalone display

#### Scenario: Install prompt is dismissible

- **WHEN** a user dismisses the install prompt
- **THEN** it is not shown again for a documented period

### Requirement: Update handling

When a new version of the application is available, the user SHALL be informed and able to update without losing unsent input.

The application MUST NOT reload itself and discard the user's work without their action.

#### Scenario: User is offered an update

- **WHEN** a new version is available
- **THEN** the user is shown a non-blocking prompt offering to reload

#### Scenario: Unsent input survives an update prompt

- **WHEN** an update becomes available while a user is composing a message
- **THEN** their draft is preserved and not discarded by the update

### Requirement: Offline tolerance

The application SHALL remain usable when connectivity is lost, keeping previously loaded conversations readable and clearly indicating that it is offline.

Real-time and interactive paths MUST always reach the network and MUST NOT be served from a cached application shell.

#### Scenario: Previously loaded content remains readable

- **WHEN** connectivity is lost
- **THEN** conversations the user has already loaded remain readable
- **AND** the application shows that it is offline

#### Scenario: Real-time paths are never served from cache

- **WHEN** the application is offline
- **THEN** requests to interactive and real-time endpoints are not answered from the cached application shell

#### Scenario: Composed messages are not silently lost

- **WHEN** a user sends a message while offline
- **THEN** the message is retained and marked as pending
- **AND** it is sent when connectivity returns, or the user is told it failed

### Requirement: Low-bandwidth operation

The application SHALL minimise data use. Media MUST NOT be downloaded at full size for display in a timeline, and the initial load MUST be bounded by a documented budget enforced in continuous integration.

Users MUST be able to restrict automatic media loading.

#### Scenario: Initial load stays within budget

- **WHEN** the application is loaded for the first time on a cold cache
- **THEN** the transferred payload is within the documented budget
- **AND** continuous integration fails if a change exceeds it

#### Scenario: User limits automatic media

- **WHEN** a user disables automatic media loading
- **THEN** media is represented by a placeholder and fetched only when the user requests it

#### Scenario: Repeat visits are cheap

- **WHEN** a returning user opens the application
- **THEN** unchanged application assets are served from cache rather than re-downloaded

### Requirement: Responsive layout

The application SHALL be usable on screens from small phones to large desktops, with all functionality reachable at every supported size.

Touch targets MUST be adequately sized, and no capability may be desktop-only.

#### Scenario: Small screen retains full function

- **WHEN** the application is used on a small phone screen
- **THEN** every capability remains reachable, with navigation adapted to the size

#### Scenario: Layout adapts without horizontal scrolling

- **WHEN** the viewport is resized across supported sizes
- **THEN** content reflows without the page scrolling horizontally

### Requirement: Theming

The application SHALL support light and dark themes, follow the operating system preference by default, and allow the user to override it.

The chosen theme MUST be applied before first paint so no flash of the wrong theme occurs, and operators MUST be able to customise branding without modifying application source.

#### Scenario: No theme flash

- **WHEN** a user with a dark theme preference loads the application
- **THEN** the correct theme is applied before first paint

#### Scenario: User override persists

- **WHEN** a user selects a theme explicitly
- **THEN** that choice persists across sessions and overrides the system preference

#### Scenario: Operator branding requires no code change

- **WHEN** an operator configures branding through supported configuration
- **THEN** the application reflects it without rebuilding from modified source

### Requirement: Accessibility

The application SHALL be operable by keyboard alone, expose semantics to assistive technologies, and meet documented contrast requirements.

Accessibility MUST be verified automatically, and regressions MUST fail continuous integration.

#### Scenario: Keyboard operation

- **WHEN** a user navigates using only the keyboard
- **THEN** every interactive element is reachable and operable, with a visible focus indicator

#### Scenario: New content is announced

- **WHEN** a message arrives in the conversation being viewed
- **THEN** assistive technologies are informed without stealing focus from the user's current position

#### Scenario: Accessibility regression fails the build

- **WHEN** a change introduces a detectable accessibility violation
- **THEN** continuous integration fails and names the violation

### Requirement: Internationalisation

The application SHALL support translation of all user-facing text, with English and Swahili provided.

The server's configured default language MUST be used for new users, and users MUST be able to choose their own.

#### Scenario: Swahili is available

- **WHEN** a user selects Swahili
- **THEN** the interface is presented in Swahili and the choice persists

#### Scenario: Server default applies to new users

- **WHEN** a new user first opens the application
- **THEN** the language is the server's configured default until they choose otherwise

#### Scenario: Untranslated text falls back

- **WHEN** a translation is missing for the selected language
- **THEN** the English text is shown rather than an empty string or a raw key

### Requirement: Guided onboarding

The application SHALL provide guided tours introducing its main areas, since users are arriving from other messaging tools.

Tours MUST be skippable, resumable, and MUST NOT block use of the application.

#### Scenario: First-time user is oriented

- **WHEN** a user signs in for the first time
- **THEN** they are offered a guided tour of the main areas

#### Scenario: Tour is skippable and does not recur

- **WHEN** a user skips or completes a tour
- **THEN** it is not presented again unless they request it

#### Scenario: Tour does not block

- **WHEN** a tour is active
- **THEN** the user can still interact with the application normally
