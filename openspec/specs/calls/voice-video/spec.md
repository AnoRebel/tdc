## Purpose

Defines optional voice and video calling between TDC users — how calls are started, joined, permissioned, and carried — as a feature an operator may disable entirely without affecting the rest of the platform.

## Requirements

### Requirement: Calling is optional

Voice and video calling SHALL be an optional capability, disabled by default, that an operator can enable without additional paid services.

When disabled, no calling interface may be presented and calling endpoints MUST reject requests.

#### Scenario: Disabled by default

- **WHEN** an operator starts a server without enabling calling
- **THEN** no calling interface is presented to users
- **AND** requests to calling endpoints are rejected as not enabled

#### Scenario: Enabling requires no paid service

- **WHEN** an operator enables calling
- **THEN** calls function between users without any commercial service being configured

#### Scenario: Core platform is unaffected

- **WHEN** calling is disabled or its infrastructure is unavailable
- **THEN** messaging, notifications, and all other capabilities continue to function

### Requirement: Starting and joining calls

Users SHALL be able to start a call in a direct conversation or a channel they are a member of, and other permitted participants MUST be able to join an ongoing call.

Starting or joining a call MUST require the same authorization as posting in that conversation, and MUST be governed by its own permission so an operator can allow messaging without allowing calls.

#### Scenario: Member starts a call

- **WHEN** a user with calling permission starts a call in a conversation they belong to
- **THEN** other members are informed a call is in progress and may join

#### Scenario: Non-member cannot join

- **WHEN** a user who is not a member of the conversation attempts to join its call without a call link
- **THEN** the request is denied

#### Scenario: Calling permission is separable

- **WHEN** an operator grants messaging but not calling to a role
- **THEN** users holding only that role can post but cannot start or join calls

### Requirement: Signalling

The server SHALL coordinate the establishment of calls by relaying session negotiation between participants, and MUST enforce authorization on every signalling exchange.

Signalling MUST NOT permit a participant to be connected to a conversation they are not authorized to join.

#### Scenario: Negotiation is relayed

- **WHEN** two authorized participants establish a call
- **THEN** the server relays their session negotiation until a connection is established

#### Scenario: Unauthorized signalling is refused

- **WHEN** a client sends signalling for a conversation it is not authorized to join
- **THEN** the server refuses to relay it

#### Scenario: Departure is signalled

- **WHEN** a participant leaves or disconnects
- **THEN** remaining participants are informed and the call state updates

### Requirement: Media transport

Media SHALL be carried directly between participants where the network permits, and MUST fall back to a relay when a direct connection cannot be established.

Media MUST be encrypted in transit, and any relay component MUST be self-hostable with no paid licence.

#### Scenario: Direct connection is preferred

- **WHEN** two participants can reach each other directly
- **THEN** media flows directly between them without traversing a relay

#### Scenario: Fallback preserves the call

- **WHEN** a direct connection cannot be established
- **THEN** media is relayed and the call proceeds

#### Scenario: Media is encrypted

- **WHEN** media flows between participants by any path
- **THEN** it is encrypted in transit

### Requirement: Group calls

The server SHALL support calls with more than two participants, and operators MUST be able to configure a maximum number of participants per call.

Where the configured limit exceeds what direct connections can sustain, a selective forwarding component MAY be used, and it MUST be optional and self-hostable.

#### Scenario: Participant limit is enforced

- **WHEN** a call reaches the configured maximum number of participants
- **THEN** further join attempts are refused with the limit explained

#### Scenario: Small calls need no extra infrastructure

- **WHEN** a call has few enough participants to be carried by direct connections
- **THEN** it functions with no forwarding component deployed

### Requirement: Call controls

Participants SHALL be able to mute and unmute audio, enable and disable video, and leave a call, with each participant's state visible to the others.

A participant MUST be able to join a call with audio only, and no client may transmit audio or video before the user has granted permission.

#### Scenario: Mute is reflected to others

- **WHEN** a participant mutes their microphone
- **THEN** other participants see them as muted and receive no audio from them

#### Scenario: Audio-only participation

- **WHEN** a participant joins without video
- **THEN** the call proceeds and other participants see them as an audio-only participant

#### Scenario: No transmission before consent

- **WHEN** a user has not granted microphone or camera permission
- **THEN** no audio or video is transmitted from that device

### Requirement: Constrained networks

Calling SHALL degrade rather than fail on poor connections, reducing quality or falling back to audio only before dropping the call.

Users MUST be warned before a call is started on a connection the client identifies as metered, where the platform exposes that information.

#### Scenario: Quality degrades before dropping

- **WHEN** a participant's available bandwidth falls
- **THEN** their media quality is reduced, and video is dropped before audio, rather than the call ending

#### Scenario: Metered connection is flagged

- **WHEN** a user starts or joins a call on a connection reported as metered
- **THEN** they are warned about data use before media begins

### Requirement: Invites and call links

Participants SHALL be able to ring people who may join a call, and a person holding the permission to do so MUST be able to make a link to a conversation's call that is either public or protected by a passcode, a lobby, or both.

Joining through a link MUST admit a person to the call only, never to the conversation, its history or its member list. People without an account MAY join through a link only where the operator has allowed guests, and a guest MUST be identified to everybody in the call as a guest.

#### Scenario: Ringing reaches every device

- **WHEN** a participant rings somebody who may join the call
- **THEN** each of that person's signed-in devices shows the incoming call, and an unanswered call leaves a missed-call notification

#### Scenario: A link admits to the call and nothing else

- **WHEN** somebody joins a call through a link
- **THEN** they take part in the call and can read nothing of the conversation beyond its name and who is in the call

#### Scenario: A protected link holds a stranger back

- **WHEN** a person opens a protected link without the passcode, or while nobody in the call has admitted them
- **THEN** they are not admitted, and repeated wrong passcodes are refused past a limit and recorded

#### Scenario: Links can be withdrawn

- **WHEN** a link has expired, been used up or been revoked
- **THEN** it admits nobody

### Requirement: Who may call whom

Each person SHALL be able to choose who may ring them or start a call with them — from their own server: everybody, people they share a conversation with, or nobody; and separately whether people on connected servers may — and the server MUST enforce that choice on every ring and every call started at them. An operator MUST be able to refuse calls from connected servers altogether, and that refusal MUST prevail over any person's choice.

A refused call MUST NOT tell the caller which setting refused it.

#### Scenario: A person who takes calls only from people they know

- **WHEN** somebody who shares no conversation with a person rings them, and that person accepts calls only from people they share a conversation with
- **THEN** the ring is refused and the person is not disturbed

#### Scenario: The operator's refusal is final

- **WHEN** the server does not accept calls from connected servers
- **THEN** no call from another server reaches anybody here, whatever their own setting

### Requirement: Incoming calls do not take over the screen

An incoming call SHALL be shown as a small window the person can move, which does not prevent them using the rest of the application, offering to answer by voice, answer with video, or decline. It MUST ring on each of the person's signed-in devices and stop on all of them when answered or declined on one.

#### Scenario: Answering on one device

- **WHEN** a person answers an incoming call on one device
- **THEN** it stops ringing on their others

### Requirement: More than one call

A person SHALL be able to be rung by several people at once and choose among them, to answer or place a call while in another — holding the first, so that nothing they send reaches it — and to switch between their calls, and to merge a held call into the active one as a conference. People merged in from another conversation MUST take part in the call only, seeing nothing of the conversation it belongs to, and MUST be asked before they are moved.

Declining a call, or ending one to take another, SHALL offer to send the caller a short message.

#### Scenario: A call arrives during a call

- **WHEN** a person in a call is rung by somebody else
- **THEN** they are told, and may decline, hold the current call and answer, end it and answer, or merge the caller into it

#### Scenario: Holding sends nothing

- **WHEN** a call is on hold
- **THEN** nothing the person sends reaches it, and its participants see them as holding

### Requirement: Screen sharing and presentations

Participants SHALL be able to share a screen, window or browser tab in a call, and one participant at a time MUST be able to present, putting their shared screen before everybody in the call.

#### Scenario: One presenter at a time

- **WHEN** a participant is presenting and another tries to present
- **THEN** the second is refused until the first stops or a moderator of the conversation clears the presentation

#### Scenario: Sharing is absent where unsupported

- **WHEN** a client's platform cannot capture a screen
- **THEN** the client does not offer sharing rather than offering it and failing

### Requirement: Remote control of a shared screen

A participant sharing their whole screen from the desktop application SHALL be able to hand control of their pointer and keyboard to one other participant at a time, who asks for it; control MUST be granted explicitly by the sharer, shown on the sharer's screen above every other window while it lasts, and revocable at once, including by a key on the sharer's keyboard that the controller cannot send. Control MUST end when the share ends or either person leaves.

Where the sharer is in a browser, is sharing less than a whole screen, or is on a platform where input cannot be injected, a request for control MUST be refused, telling the person asking which of these it is. No input may be applied to the sharer's computer unless a grant to its sender is in force.

#### Scenario: Control is asked for and granted

- **WHEN** a participant asks to control a whole screen shared from the desktop application
- **THEN** the sharer is asked, and only on their allowing it does the participant's input reach the sharer's computer

#### Scenario: Taking control back

- **WHEN** the sharer takes control back, presses the reserved key, stops sharing, or either leaves the call
- **THEN** control ends at once, and input sent after it is not applied

#### Scenario: Refused where it cannot be given

- **WHEN** the sharer is in a browser, shares a window or a tab, or is on a platform that cannot inject input (such as a Wayland session)
- **THEN** the request is refused, saying which

### Requirement: Privacy and recording

The server SHALL NOT record calls unless recording is explicitly enabled by the operator, and where enabled, every participant MUST be informed before recording begins.

#### Scenario: No recording by default

- **WHEN** calling is enabled without recording being configured
- **THEN** no call media is retained

#### Scenario: Recording needs its own permission

- **WHEN** recording is enabled and a participant without permission to record attempts it
- **THEN** the attempt is refused, naming the permission

#### Scenario: A recording is kept with the conversation

- **WHEN** a recording ends
- **THEN** it is saved to the conversation like any attachment, under the same limits and retention, or offered to the recorder as a download when it is larger than the server accepts

#### Scenario: Somebody joining is told first

- **WHEN** a person joins a call that is being recorded
- **THEN** they are told before their own audio or video is sent

#### Scenario: Recording is disclosed

- **WHEN** a call is recorded
- **THEN** all participants are informed before recording starts
- **AND** the fact of recording is written to the audit log

#### Scenario: Recording needs the operator's setting as well

- **WHEN** recording is not enabled on the server and anybody, an administrator included, attempts it
- **THEN** the attempt is refused, saying recording is not enabled

#### Scenario: A recording ends with its recorder

- **WHEN** the person recording stops, leaves the call, holds it, or their connection drops
- **THEN** the recording stops, and its end is written to the conversation's log and the audit log

#### Scenario: Recording is made on the recorder's device

- **WHEN** a call is recorded
- **THEN** the file is made by the recorder's client from what it receives, and no call media passes through the server for it beyond the attachment saved afterwards
