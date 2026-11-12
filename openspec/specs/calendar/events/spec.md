## Purpose

Defines events — meetups, talks, workshops, office hours and other gatherings — on a TDC server: how they are created and managed, who may see and answer them, how people are reminded of them, how they are hosted in TDC's own calls, and how they move between TDC and the calendars people already use.

## Requirements

### Requirement: Events are optional

Events SHALL be a capability an operator can turn off. It MUST be on by default, since it needs no service outside the server. While it is off no events interface may be presented and every events route, feed included, MUST refuse requests as not offered.

#### Scenario: Turned off

- **WHEN** an operator turns the events capability off
- **THEN** clients show no events interface
- **AND** every events route and feed answers that the server does not offer it
- **AND** existing events are kept, and reappear when it is turned back on

### Requirement: Creating and managing events

A person holding the permission to create events SHALL be able to create an event of a kind — meetup, talk, workshop, office hours or other — with a title, a description in the message formatting syntax, a start and an end in a named time zone, an optional place, an optional online meeting, an optional capacity, a visibility, and an optional cover image. An event SHALL belong to one channel or to the whole server. Its creator and its hosts MUST be able to edit and cancel it; deleting it MUST take its creator or the permission to manage any event. Acting on somebody else's event under that permission MUST be recorded in the audit log.

#### Scenario: A member schedules a meetup in a channel

- **WHEN** a member allowed to create events schedules a meetup in a channel they can read
- **THEN** the event is created in that channel
- **AND** it is shown to the channel's members in the channel and on the Events page

#### Scenario: Somebody else's event cannot be edited

- **WHEN** a person who neither created nor hosts an event, and holds no permission to manage events, tries to edit or cancel it
- **THEN** the change is refused

#### Scenario: A manager acts on another person's event

- **WHEN** a person holding the permission to manage any event cancels somebody else's event
- **THEN** the event is cancelled
- **AND** the audit log records who cancelled it

#### Scenario: Cancelling keeps the record

- **WHEN** an event is cancelled with a reason
- **THEN** it remains visible as cancelled, with the reason
- **AND** it can no longer be answered

### Requirement: Hosts, speakers and agenda

An event SHALL have hosts and speakers chosen from the server's people, and MAY have an agenda of sessions — the talks or parts within it — each with a title, a description, a time within the event, and its speakers, who may include people not on the server, named in words.

#### Scenario: A meetup with two talks

- **WHEN** a host adds two sessions with their speakers to a meetup
- **THEN** the event page shows the agenda in order of time with each session's speakers

### Requirement: Time zones

An event's times SHALL be stored as absolute instants together with the event's time zone. Clients MUST show times in the reader's own zone and, whenever the event's zone differs, in the event's zone as well. An all-day event MUST be treated as whole days in its own zone and never shifted by the reader's zone.

#### Scenario: Read from another zone

- **WHEN** an event at 18:00 in Africa/Dar_es_Salaam is read by somebody whose zone is Europe/London
- **THEN** they see the time in London time and the organiser's 18:00 EAT beside it

#### Scenario: An all-day event

- **WHEN** an all-day event on a date is read from a zone hours away
- **THEN** it is shown on that date and no other

### Requirement: Repeating events

An event SHALL be able to repeat daily, weekly or monthly, at an interval, on chosen weekdays or days of the month, ending after a number of occurrences or on a date or not at all, with chosen occurrences skipped. Occurrences MUST keep their local time in the event's zone across daylight-saving changes. A rule outside this set MUST be refused when written.

#### Scenario: Weekly across a daylight-saving change

- **WHEN** a weekly 19:00 event in Europe/London spans the end of summer time
- **THEN** every occurrence starts at 19:00 London time

#### Scenario: A skipped week

- **WHEN** a host skips one occurrence of a weekly event
- **THEN** that occurrence is not listed, exported or reminded of
- **AND** the others are unchanged

### Requirement: Visibility

An event SHALL be visible to its creator, hosts and speakers, to people allowed to manage events where it is, and otherwise according to its visibility: everybody on the server for a public event, the members of its channel who may read it for a channel event, and the people invited — directly or through an invited channel — for an invite-only event. A public event MUST NOT belong to a private channel. An event somebody may not see MUST be answered exactly as one that does not exist, on every route and in every feed.

#### Scenario: Invite-only stays closed

- **WHEN** somebody not invited asks for an invite-only event
- **THEN** the server answers as if the event did not exist

#### Scenario: Channel event outside the channel

- **WHEN** somebody who is not a member of a channel lists events
- **THEN** that channel's events are not among them

#### Scenario: A public event in a private channel

- **WHEN** somebody tries to make an event public in a private channel
- **THEN** the request is refused

### Requirement: Invitations and answers

Hosts SHALL be able to invite people and channels to an event, and the people invited MUST be notified. People who can see an event MUST be able to answer going, maybe or not going, and change their answer until the event has ended or been cancelled. An answer to a repeating event SHALL cover the series.

#### Scenario: Inviting a channel

- **WHEN** a host invites a channel to an invite-only event
- **THEN** that channel's members can see and answer it
- **AND** they are told they were invited

### Requirement: Capacity and waiting list

An event with a capacity SHALL admit no more people going than its capacity. Answering going to a full event MUST put the person on a waiting list, in the order people answered; when a place frees, the first person waiting MUST become going and be told. Two people answering for the last place at once MUST NOT both be admitted.

#### Scenario: Full

- **WHEN** a person answers going to an event whose places are taken
- **THEN** they are put on the waiting list and told their place

#### Scenario: A place frees

- **WHEN** somebody going changes their answer
- **THEN** the first person waiting becomes going and is notified

### Requirement: Attendee privacy

Each person SHALL be able to choose whether others may see which events they are going to. An attendee list MUST name only people who allow it and count the others; hosts and people allowed to manage events MUST see everybody. A person's answer MUST NOT be disclosed to integrations when they have chosen not to be shown.

#### Scenario: Hidden attendance

- **WHEN** a person who hides their attendance answers going
- **THEN** other attendees see the count include them without their name
- **AND** the event's hosts see their name

### Requirement: Reminders

Before each occurrence of an event, at the lead times the operator sets, the people going or maybe going and its hosts and speakers SHALL be reminded through the notification system: in the application, by web push where they have enabled it, as a desktop notification from the desktop application, and in email summaries for people who chose them. A reminder MUST be sent at most once per occurrence and lead time across every node, MUST NOT be sent for an occurrence that has started, and MUST NOT be sent to somebody who has muted the event. Changes to an event's time or place, and its cancellation, MUST be notified to the people going.

#### Scenario: A day before

- **WHEN** an event the person is going to starts in a day
- **THEN** they are reminded once, on every device they are reachable on

#### Scenario: Several nodes

- **WHEN** two nodes run their reminder pass at the same moment
- **THEN** each reminder is sent once

### Requirement: Hosting an event in a call

An event MAY be hosted online in TDC. Such an event SHALL have a conversation of its own whose members are its hosts, speakers and the people going or maybe going, and whose call is the event's call: started by a host from the event's page, and joined from there by attendees. It MUST be an ordinary call, with every rule calls have, and a host allowed to make call links MUST be able to make one for guests from outside the server. Nobody may join an event's conversation except by answering the event.

#### Scenario: Starting and joining

- **WHEN** a host starts the event's call from its page
- **THEN** attendees see that it has started and join it from the event page

#### Scenario: Attendees cannot start it

- **WHEN** an attendee who is not a host tries to start the event's call
- **THEN** the request is refused

### Requirement: Calendar export

Every event SHALL be downloadable as an iCalendar file. Every person SHALL be able to make a private subscription address for the events they are going to, hosting or speaking at, and their channels' events, which any calendar application can subscribe to; the address MUST be shown once, stored only as a hash, revocable, and its contents decided for its owner at every fetch. An operator MAY publish the server's public events as a feed. Exported files MUST carry each event's zone, repeat, skipped occurrences and cancellation so that another calendar shows the same instants.

#### Scenario: Subscribing in a calendar application

- **WHEN** a person adds their subscription address to a calendar application
- **THEN** the events they are going to appear there at the right local times
- **AND** an event they leave, or a channel they leave, disappears from it on the next refresh

#### Scenario: A revoked address

- **WHEN** a person revokes their subscription address
- **THEN** fetching it is refused

### Requirement: Calendar import

A person allowed to create events SHALL be able to import an iCalendar file, seeing first what would be created, updated or skipped. Events MUST be matched by their UID so that importing a file twice creates nothing twice, and an event somebody else made MUST NOT be overwritten. Time zones, all-day events and repeats within the supported set MUST be preserved, and anything that cannot be MUST be reported in the preview.

#### Scenario: Importing twice

- **WHEN** a person imports the same file twice
- **THEN** the second import updates rather than duplicates

### Requirement: Calendar subscriptions

A person holding the permission to manage calendars SHALL be able to subscribe the server, or a channel, to an external calendar address. The server MUST refresh it periodically, creating, updating and cancelling events by UID, and MUST fetch it with the protections every outbound request has: private and reserved addresses refused at connection time, redirects bounded and judged the same way, size and time limited.

#### Scenario: A private address

- **WHEN** somebody subscribes to an address that resolves to a private network
- **THEN** the subscription is refused
- **AND** a public address that later redirects to a private one is refused at fetch time

### Requirement: Event integrations

Outgoing webhooks subscribed to a channel SHALL be able to receive the creation, change, cancellation of, and answers to, that channel's events, except for invite-only events, which MUST NOT be delivered to a channel's subscriptions.

#### Scenario: A channel's webhook

- **WHEN** a channel event is created and a webhook on that channel asked for event creations
- **THEN** the webhook receives it
