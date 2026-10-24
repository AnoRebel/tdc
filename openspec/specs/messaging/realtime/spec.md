## Purpose

Defines how clients receive messages and state changes as they happen — connection lifecycle, subscriptions, presence and status, delivery and read state, and recovery after disconnection — with the network and battery constraints of mobile users on metered data treated as first-class requirements.

## Requirements

### Requirement: Live delivery

A connected client SHALL receive new messages and state changes for its subscriptions without polling, and delivery MUST be scoped so a client only receives events it is authorized to see.

#### Scenario: Message reaches subscribers promptly

- **WHEN** a message is posted to a channel
- **THEN** connected members of that channel receive it without issuing a new request

#### Scenario: Delivery respects authorization

- **WHEN** an event occurs in a channel a connected user is not a member of
- **THEN** that event is not delivered to that user's connection

#### Scenario: Authorization change affects live delivery

- **WHEN** a user's access to a channel is revoked while connected
- **THEN** subsequent events for that channel are not delivered to them

### Requirement: Subscriptions

A client SHALL declare which conversations it wants live updates for, and MUST be able to change that set without tearing down its connection.

The server MUST NOT require a client to subscribe to everything it is permitted to see.

#### Scenario: Subscription set changes in place

- **WHEN** a client subscribes to an additional channel
- **THEN** it begins receiving that channel's events on the existing connection

#### Scenario: Client subscribes selectively

- **WHEN** a client is a member of many channels but subscribes to a few
- **THEN** it receives events only for the subscribed channels
- **AND** it can still be informed of activity elsewhere through lightweight notification rather than full event delivery

### Requirement: Reconnection and gap recovery

After a disconnection, a client SHALL be able to resume from its last known position and receive exactly the events it missed, without duplicates and without re-fetching entire histories.

Where the gap is too large to replay, the server MUST tell the client to resynchronise rather than silently omitting events.

#### Scenario: Missed events are replayed

- **WHEN** a client reconnects after a brief disconnection
- **THEN** it receives the events it missed, in order, with no duplicates

#### Scenario: Oversized gap is signalled

- **WHEN** a client reconnects after missing more events than the server retains for replay
- **THEN** the server instructs the client to resynchronise rather than delivering a partial, gap-containing stream

#### Scenario: Reconnection does not duplicate

- **WHEN** a client reconnects and replays
- **THEN** no event it already processed is delivered again

### Requirement: Connection efficiency on constrained networks

The transport SHALL be efficient enough for use on metered mobile data and intermittent connectivity. Payloads MUST be compact, and the client MUST NOT be required to maintain a continuously open connection to remain usable.

Idle connections MUST NOT generate constant traffic; keepalive intervals MUST be configurable by the operator.

#### Scenario: Idle connection is quiet

- **WHEN** a client is connected with no activity in its subscriptions
- **THEN** traffic is limited to infrequent keepalives at the operator-configured interval

#### Scenario: Application remains usable when disconnected

- **WHEN** a client loses connectivity
- **THEN** previously loaded conversations remain readable
- **AND** the client indicates it is disconnected and reconnects automatically when connectivity returns

#### Scenario: Reconnection avoids thundering herd

- **WHEN** many clients are disconnected simultaneously and the server returns
- **THEN** clients reconnect with randomised backoff rather than simultaneously

### Requirement: Presence

The server SHALL track whether a user is currently connected and expose that to users permitted to see it.

Presence MUST be derived from actual connection state, MUST expire if a connection is lost without a clean disconnect, and users MUST be able to appear offline regardless of actual connection state.

#### Scenario: Presence reflects connection

- **WHEN** a user connects
- **THEN** users permitted to see their presence observe them as online

#### Scenario: Ungraceful disconnect expires

- **WHEN** a client's connection is lost without a clean disconnect
- **THEN** the user's presence returns to offline within a bounded time rather than remaining online indefinitely

#### Scenario: Invisibility is honoured

- **WHEN** a user chooses to appear offline
- **THEN** other users observe them as offline while they remain fully functional

### Requirement: Status

Users SHALL be able to set a status such as available, busy, or away, and operators MUST be able to configure the available statuses, including their labels and whether each suppresses notifications.

A status MAY carry an expiry after which it reverts automatically.

#### Scenario: Operator configures statuses

- **WHEN** an operator defines the set of selectable statuses
- **THEN** users may choose only from that set
- **AND** each status behaves as configured with respect to notification suppression

#### Scenario: Status expires

- **WHEN** a user sets a status with an expiry and that time passes
- **THEN** the status reverts automatically without user action

#### Scenario: Busy suppresses notifications

- **WHEN** a user holds a status configured to suppress notifications
- **THEN** notifications are withheld or downgraded per that configuration

#### Scenario: Appearing offline does not undo a status

- **WHEN** a user holds a status configured to suppress notifications and also chooses to appear offline
- **THEN** other users see neither the status nor its note
- **AND** notifications remain suppressed
- **AND** the user is shown their own status, its note, its expiry and that they appear offline

#### Scenario: A client offers the expiry

- **WHEN** a user sets a status in either client
- **THEN** they may choose when it clears — after a fixed time, at the end of the day or week, at a chosen time, or never — and are shown how long it has left

### Requirement: Location

A user SHALL be able to state where they are as a place name on their account, either typed by them or detected from their device, and SHALL choose who may see it: anyone permitted to see them (including people on other servers), members of this server, or nobody. It MUST default to nobody.

Detection MUST resolve a position to a place name on the user's own server, from data the server carries, without contacting any third party; only the place name MAY be stored, and the position MUST NOT be stored or logged. A client MUST NOT prompt for the device's position except when the user asks for detection; it MAY refresh a detected place periodically only where permission was already granted.

Every response that describes a person to somebody else MUST apply the same visibility rule, and MUST omit the location where the viewer is not entitled to it rather than send it empty. Where the location appears inside something with its own visibility, such as a directory profile, it MUST NOT be more visible than that container.

#### Scenario: Location starts hidden

- **WHEN** a user has never changed their location setting
- **THEN** nobody else is shown a location for them

#### Scenario: Members-only location is withheld from outsiders

- **WHEN** a user's location is visible to members and somebody not signed in to this server views them
- **THEN** the response describing that user carries no location, and a search by location does not find them

#### Scenario: Detection keeps only a place name

- **WHEN** a user asks for their location to be detected and allows it
- **THEN** the server stores the name of the nearest town and nothing about the position it was given

#### Scenario: Detection unavailable

- **WHEN** a device refuses or cannot provide its position
- **THEN** the client says so and offers to have the user type their location instead

### Requirement: Automatic replies

Users SHALL be able to keep automatic replies that the server posts on their behalf while they are away, in answer to a direct message or to being mentioned in a channel. Each reply MUST carry its text (at most 500 characters), and MAY be tied to one or more statuses, to a window of time with either end open, or to both; a reply tied to nothing applies until it is turned off. The server MUST mark every automatic reply in the message's own content as automatic, so that no client, export or delivery presents it as the person typing.

The server MUST NOT post an automatic reply to a message from a program, a webhook, another automatic reply, or the person themselves; MUST answer a given sender at most once per conversation per day; MUST NOT answer somebody either party has blocked or whom the person's direct-message setting would not admit; and MUST NOT post a reply tied to a status while the person appears offline.

#### Scenario: A direct message is answered once

- **WHEN** somebody sends a direct message to a person whose automatic reply applies
- **THEN** the server posts the reply in that conversation as the person, marked as automatic
- **AND** a further message from the same sender in that conversation within a day is not answered again

#### Scenario: A mention is answered in its thread

- **WHEN** somebody mentions a person whose automatic reply applies in a channel they share
- **THEN** the reply is posted in the thread of the mentioning message, using the mention text if one was given
- **AND** nothing is posted in the channel itself

#### Scenario: Outside its window or status

- **WHEN** a reply's window has not begun or has ended, or the person does not hold any of the statuses it is tied to
- **THEN** no reply is posted

#### Scenario: Programs and automatic replies are never answered

- **WHEN** a bot, a webhook, or another person's automatic reply addresses a person whose automatic reply applies
- **THEN** no reply is posted

#### Scenario: Appearing offline is not contradicted

- **WHEN** a person appears offline and holds a status that one of their replies is tied to
- **THEN** that reply is not posted, since the status is withheld from everybody
- **AND** a reply tied to appearing offline, or to no status, still answers, and nothing about any reply depends on whether the person is connected

#### Scenario: Blocks and contact settings are honoured

- **WHEN** the sender has blocked the person, the person has blocked the sender, or the person's direct-message setting would not admit the sender
- **THEN** no reply is posted, and the silence is indistinguishable from the person not having one

#### Scenario: Set from the status dialog

- **WHEN** a person chooses a status other than available in either client
- **THEN** they may turn on an automatic reply tied to that status, with its text, which stops answering when the status changes back or clears
- **AND** while any reply applies, the client says so where the person's status is shown

### Requirement: Typing indication

The server SHALL support indicating that a user is composing a message in a conversation. Indicators MUST expire automatically if not refreshed, and MUST be suppressible by the user.

#### Scenario: Indicator expires without refresh

- **WHEN** a client stops refreshing a typing indication
- **THEN** the indication disappears for other participants within a bounded time

#### Scenario: Indication can be disabled

- **WHEN** a user disables sending typing indication
- **THEN** no such indication is sent on their behalf

#### Scenario: The setting is the account's

- **WHEN** a user turns off typing indication in either client's privacy settings
- **THEN** their clients stop announcing typing
- **AND** the server records nothing from any client that still announces it

### Requirement: Delivery and read state

The server SHALL track each user's read position per conversation so unread state is consistent across that user's devices.

Read state MUST converge when a user reads on one device while others are connected or offline.

#### Scenario: Read state syncs across devices

- **WHEN** a user reads a conversation on one device
- **THEN** their other devices reflect the updated read position

#### Scenario: Unread counts are accurate

- **WHEN** a user has unread messages across several conversations
- **THEN** the reported unread counts match the messages after their read position, excluding their own messages

#### Scenario: Offline device converges on reconnect

- **WHEN** a device that was offline reconnects
- **THEN** it converges on the read position established elsewhere rather than resurfacing already-read messages

### Requirement: Multi-node operation

The delivery mechanism SHALL support running more than one server process behind a load balancer, with an event reaching subscribers regardless of which process they are connected to.

A single-process deployment MUST NOT require any additional infrastructure to achieve this.

#### Scenario: Cross-process delivery

- **WHEN** two clients are connected to different server processes and one posts to a shared channel
- **THEN** the other receives the message

#### Scenario: Single node needs nothing extra

- **WHEN** an operator runs a single server process
- **THEN** real-time delivery works with only the server and its database, with no broker or additional service required
