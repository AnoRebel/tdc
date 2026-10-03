## Purpose

Defines the TDC web application — an installable, offline-tolerant progressive web app built for mid-range phones on metered mobile data, since that is how most TDC members will reach the platform.

## Requirements

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

### Requirement: Attachment viewing

The client SHALL display an attachment's content in place rather than requiring a download, for every type it can render: images, animated images, video, audio, and PDF documents.

A viewer MUST open from the conversation without navigating away from it, MUST be dismissable by keyboard as well as by pointer, and MUST allow moving between the attachments of one message without closing and reopening.

An attachment the client cannot render MUST still show its name, size and type, and offer to download it, rather than showing an error or an empty frame.

Rendering MUST NOT grant uploaded content the privileges of the application: an attachment is displayed in a context that cannot reach the application's session, storage, or other attachments.

#### Scenario: An image opens in place

- **WHEN** a participant selects an image attachment
- **THEN** it is displayed at full size over the conversation
- **AND** dismissing it returns to the conversation where it was

#### Scenario: Video and audio play without downloading

- **WHEN** a participant plays a video or audio attachment
- **THEN** playback begins without first downloading the whole file

#### Scenario: A document is readable in place

- **WHEN** a participant opens a PDF attachment
- **THEN** its pages are displayed without leaving the application

#### Scenario: Several attachments are navigable

- **WHEN** a message carries more than one viewable attachment
- **THEN** a participant can move between them from within the viewer

#### Scenario: An unviewable file degrades gracefully

- **WHEN** an attachment's type cannot be rendered
- **THEN** its name, size and type are shown with the option to download it

#### Scenario: The viewer is dismissable by keyboard

- **WHEN** a participant presses the dismiss key
- **THEN** the viewer closes and focus returns to where it was opened from

### Requirement: Upload feedback

The client SHALL show the progress of an upload while it is in flight, and MUST let the person cancel it before it completes.

Progress MUST be reported from the transfer itself rather than simulated, and a failed upload MUST say why and offer to retry without the person selecting the file again.

An upload MUST NOT block the conversation: a participant can continue reading and writing while a file is transferring.

#### Scenario: Progress reflects the transfer

- **WHEN** a file is uploading
- **THEN** the proportion transferred is displayed and advances as the transfer does

#### Scenario: An upload can be cancelled

- **WHEN** a participant cancels an upload in flight
- **THEN** the transfer stops and nothing is attached

#### Scenario: A failure is explained and retryable

- **WHEN** an upload fails
- **THEN** the reason is shown
- **AND** the participant can retry without selecting the file again

#### Scenario: A refused file says which limit it broke

- **WHEN** the server refuses a file for exceeding a limit
- **THEN** the client states which limit and what the file was

#### Scenario: The conversation stays usable

- **WHEN** an upload is in flight
- **THEN** the participant can still read and send messages

### Requirement: Confirmation before destructive actions

An action that destroys something a person cannot recover SHALL require a distinct confirmation before it happens, and the confirmation MUST name what is about to be lost.

Confirmation MUST NOT be the default-focused choice, so that a reflexive keypress cannot complete a destruction, and the confirming control MUST be visibly distinct from the one that cancels.

An action that is reversible — leaving a channel, muting, hiding — MUST NOT demand confirmation, because a prompt on everything trains people to dismiss prompts without reading them.

#### Scenario: Deleting a message is confirmed

- **WHEN** a participant deletes a message
- **THEN** they are asked to confirm
- **AND** the message is named or shown in the prompt

#### Scenario: Cancelling leaves everything intact

- **WHEN** a participant dismisses a confirmation
- **THEN** nothing is destroyed and the interface returns to where it was

#### Scenario: A reflexive keypress does not destroy

- **WHEN** a confirmation appears
- **THEN** the destructive choice is not the one a plain Enter or Escape activates

#### Scenario: Reversible actions are not interrupted

- **WHEN** a participant takes an action they can undo
- **THEN** no confirmation is required

### Requirement: Contextual actions

A message, attachment, conversation and person SHALL each offer their available actions from a context menu, opened by right-click with a pointer and by a long press on a touch screen.

Every action reachable from a context menu MUST also be reachable another way, so that the menu is a shortcut and never the only route.

A context menu MUST close on Escape and on a click outside it, MUST be navigable by keyboard, and MUST NOT offer an action the person is not permitted to take.

#### Scenario: A message offers its actions

- **WHEN** a participant opens the context menu on a message
- **THEN** the actions available to them on that message are listed

#### Scenario: Long press opens the menu on a touch screen

- **WHEN** a participant presses and holds a message on a touch screen
- **THEN** the same menu opens

#### Scenario: Unavailable actions are absent

- **WHEN** a participant opens the menu on someone else's message
- **THEN** actions they are not permitted to take are not shown

#### Scenario: The menu is dismissable

- **WHEN** a context menu is open and the participant presses Escape or clicks away
- **THEN** the menu closes and nothing is acted on

#### Scenario: Actions remain reachable without the menu

- **WHEN** a participant does not use the context menu
- **THEN** every action it offers is still reachable

### Requirement: Touch interaction

The client SHALL be operable by touch throughout, and every action available with a pointer MUST be available with a finger.

Interactive targets MUST be large enough to hit reliably, and an action that depends on hovering MUST have a touch equivalent, since a touch screen has no hover.

A swipe gesture MAY offer a shortcut to a common action, and where one does, it MUST be discoverable another way and MUST NOT be the only route to that action.

#### Scenario: Hover-only actions have a touch route

- **WHEN** an action is revealed by hovering with a pointer
- **THEN** a touch user can reach the same action

#### Scenario: Targets are large enough

- **WHEN** the interface is used on a touch screen
- **THEN** interactive targets meet the documented minimum size

#### Scenario: A swipe is a shortcut, not a requirement

- **WHEN** a swipe gesture performs an action
- **THEN** that action is also reachable without gesturing

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
