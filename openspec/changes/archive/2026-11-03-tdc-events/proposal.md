## Why

A developer community lives by its gatherings: the monthly meetup, a talk somebody gives on a Thursday evening, a workshop a chapter runs, a maintainer's office hours. Today a TDC server can talk about all of them and hold none. Each is announced in a channel as a message, its RSVPs are reactions nobody can count, its time is written in whatever zone its author happened to be in, and it reaches people's calendars only if somebody copies it there by hand. The meeting itself is a link to another product, even though TDC now has calls.

## What Changes

This change adds `calendar/events`: events that belong to a channel or to the whole server, that people answer and are reminded of, that are hosted in TDC's own calls, and that move in and out of the calendars people already use.

- **Events.** Meetups, talks, workshops, office hours and other gatherings, each with a title, a description written in the same Markdown as messages, a start and end in a named time zone, an optional repeat (daily, weekly or monthly, ending after a number of times or on a date, with occurrences skipped), a place and/or an online meeting, a capacity, a cover image, hosts and speakers, and an agenda of sessions (the talks within a meetup). Created, edited, cancelled and deleted by whoever is allowed to.
- **Where events live.** An event belongs to a channel, and is shown there, or to the server. Its visibility is *public* (everybody on the server), *the channel's members*, or *invited people only*. An Events page lists them as an agenda, a month and a week, upcoming and past; each event has its own page.
- **Times that mean the same thing everywhere.** Stored as instants in UTC with the event's own zone beside them; shown in the reader's zone, with the event's zone stated whenever the two differ.
- **Invitations and answers.** Hosts invite people and whole channels; people answer going, maybe or not going; a full event puts further "going" answers on a waiting list and moves people up as places free. Attendee lists respect a new per-person privacy choice — whether others may see which events you are going to.
- **Reminders.** Before each occurrence, the people going (and hosts) are reminded through the existing notification system: in the application, by web push, as a desktop banner, and in email summaries for those who chose them.
- **Hosting.** An event can be hosted online in TDC: it has a conversation of its own whose call its hosts start from the event page and attendees join from there. Nothing about calls is reimplemented; the event's conversation is an ordinary call-capable conversation, and hosts holding `call.link` make links for guests from outside the server through the existing call links.
- **Permissions.** `event.create` (every member, by the Member baseline), `event.manage.any` (nobody by default) and `event.calendars.manage` (nobody by default), enforced on every route; the sensitive acts audited.
- **Calendars in and out.** Every event downloads as an `.ics` file; every person has a private subscription address (webcal, revocable) for the events they are going to and their channels' events; a server may publish its public events as a feed. `.ics` files import with a preview, de-duplicated by their UID, with their time zones and repeats; external calendars (any ICS address) can be subscribed to and are refreshed periodically, fetched with the same private-network guard as webhooks. Google Calendar, Outlook and Apple Calendar are reached through subscription and "add to calendar" links rather than per-person OAuth, and the user guide says exactly how.
- **Integrations.** `event.created`, `event.updated`, `event.cancelled` and `event.rsvp` reach outgoing webhooks subscribed to the event's channel.
- **Switch.** An `events` feature, on by default, that hides and refuses all of the above when off.

**Out of scope:** two-way synchronisation with Google or Microsoft accounts through their APIs; RSVPs per occurrence of a repeating event (an answer covers the series); CalDAV.

## Capabilities

### New Capabilities

- `calendar/events`: events, visibility, recurrence, invitations and answers, reminders, hosting, and calendar import and export.

### Modified Capabilities

- `identity/authorization`: `event.create`, `event.manage.any`, `event.calendars.manage`.
- `platform/server-config`: the `events` switch and its settings.
- `platform/notifications`: invitation, reminder, change, cancellation and waiting-list reasons.
- `integrations/webhooks`: the event types above.

## Impact

- **Dependencies:** `github.com/arran4/golang-ical` (Apache-2.0) to read and write iCalendar; `github.com/teambition/rrule-go` (MIT) for repeat rules. Nothing AGPL, nothing paid.
- **Database:** one migration adding the calendar tables, an `event` room kind, a privacy column and notification reasons.
- **Operators:** the hosting guide documents the switch, reminder lead times, the public feed (off by default) and external subscriptions (which make the server fetch addresses people give it).
- **Security:** feed addresses are bearer secrets, stored hashed and revocable; subscription fetches go through the connect-time private-address guard; every route checks visibility as well as permission, and refuses an event somebody cannot see exactly as one that does not exist.
