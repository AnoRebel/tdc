Scope note: these tasks implement `calendar/events` against the running platform, and the parts of `identity/authorization`, `platform/server-config`, `platform/notifications` and `integrations/webhooks` it touches. The canonical specs are edited directly. Tasks reference the spec for what to build and `design.md` for how.

**Documentation rule: documentation is written as the work is done.** The user guide's Events section, the hosting guide's settings and the integration guide's feeds and webhook types stay current with each phase.

**Phasing rule: every phase ends in a running, demonstrable system**, and a server with the `events` feature off behaves exactly as before.

## 1. Phase 1 — Events exist

- [x] 1.1 Migration: calendar tables, the `event` room kind, `event.create` for Member; `events` feature (on by default) and its settings; `event.create`, `event.manage.any`, `event.calendars.manage` in the catalogue; verify with the feature off every events route answers 404 and nothing is advertised
- [x] 1.2 Event rooms: created with the event, private, excluded from room lists, search and counts; room invitations refused; verify an event's room never appears in anybody's conversation list
- [x] 1.3 Create, read, edit, cancel and delete, with hosts, speakers and sessions; who may do each; audited where another person's event is acted on; verify a member cannot edit an event they neither made nor host, and a manager can
- [x] 1.4 Visibility in one function, used by every route; public only in public channels; verify an invite-only event is answered as missing to somebody not invited, and a channel event to somebody outside the channel
- [x] 1.5 Time zones and repeats: the RRULE subset validated, expanded in the event's zone with exceptions, `last_ends_at` kept; verify a weekly series keeps its local time across a daylight-saving change, and in EAT against a UTC database
- [x] 1.6 `GET /api/events` over a range, occurrences expanded; routes in the API catalogue and the OpenAPI bodies regenerated
- [x] 1.7 Clients: Events page (agenda, month, week; upcoming and past; channel filter), event page, create and edit dialog (TanStack Form, Valibot), a channel's next events in its header, the sidebar entry; both locales; payload budget not raised
- [x] **MILESTONE 1** — Demonstrate: a member schedules a weekly meetup in a channel in Dar es Salaam time, somebody in London sees it at their own time with the organiser's beside it, and somebody outside the channel cannot find it.
  Demonstrated by `scripts/demo-milestone-events-1.sh`.

## 2. Phase 2 — Invitations, answers and reminders

- [x] 2.1 Answers with capacity and the waiting list in a transaction; event-room membership kept in step; verify two people answering for the last place cannot both have it, and the waiting list moves up when a place frees
- [x] 2.2 Attendee lists under `showsEventAttendance`; verify a person who hides attendance is counted and not named, except to hosts
- [x] 2.3 Invitations of people and channels, with `event_invite` notifications; verify an invited channel's members can see an invite-only event and others cannot
- [x] 2.4 Reminders: the pass, its claim, `event.reminder` in the log, notification, push, live frame; desktop banner; digests; verify one reminder per occurrence and lead across two passes, none for a past occurrence, none for a muted event
- [x] 2.5 Change, cancellation and promotion notifications
- [x] 2.6 Webhooks: the four types in the catalogue; forwarded to the event's channel's subscriptions; never for invite-only events
- [x] **MILESTONE 2** — Demonstrate: a host invites a channel to a twenty-place workshop; the twenty-first person is waitlisted and moves up when somebody declines; everybody going is reminded a day before on web and desktop.
  Demonstrated by `scripts/demo-milestone-events-2.sh`.

## 3. Phase 3 — Hosting

- [x] 3.1 The event's call: started by a host from the event page, joined by attendees from it; verify an attendee cannot start it, and joins once a host has
- [x] 3.2 Guest links from the event page for hosts holding `call.link`, through the existing call links
- [x] 3.3 Cover images, uploaded to the event's conversation, shown to anybody who can see the event, never reclaimed as an orphan
- [x] **MILESTONE 3** — Demonstrate: a host starts a talk's call from its page; two attendees join from theirs; a guest joins through a link.
  Demonstrated by `scripts/demo-milestone-events-3.sh`.

## 4. Phase 4 — Calendars out

- [x] 4.1 `.ics` writer with `VTIMEZONE` for repeating events; per-event download; "Add to Google Calendar" and "Add to Outlook" links
- [x] 4.2 Personal feed address: make, show once, revoke, audited; fetch decided for its owner on every request; verify a revoked address refuses and a left channel's events leave the feed
- [x] 4.3 Public feed behind `events.public_feed`
- [x] 4.4 Verify an exported file parses in a second implementation (Python `icalendar`) with the same instants, repeats and zones
- [x] **MILESTONE 4** — Demonstrate: a person adds their feed to a calendar application and sees their meetups there at the right local time.
  Demonstrated by `scripts/demo-milestone-events-4.sh`.

## 5. Phase 5 — Calendars in

- [x] 5.1 `.ics` import with a preview: create, update or skip by UID, zones resolved (IANA, Windows names, fixed `VTIMEZONE`), all-day events, repeats in the subset, warnings for the rest
- [x] 5.2 Subscriptions to external calendars: made with `event.calendars.manage`, fetched through the private-address guard with size, time and redirect limits, refreshed on a schedule claimed by one node, upserted by UID, vanished events cancelled, paused after repeated failure; verify a subscription to a private address is refused at creation and at fetch, including after a redirect
- [x] **MILESTONE 5** — Demonstrate: an exported Google Calendar file imports with its weekly event at the right times, and a subscribed community calendar's new event appears after a refresh.
  Demonstrated by `scripts/demo-milestone-events-5.sh`.

## 6. Phase 6 — Ready

- [x] 6.1 User guide: Events, answering, reminders, hosting, adding the feed to Google Calendar, Outlook and Apple Calendar, importing
- [x] 6.2 Hosting guide: the switch, reminder lead times, public feed, subscriptions and what they fetch
- [x] 6.3 Integration guide: the API, feeds and webhook types
- [x] 6.4 Browser checks of creating, answering, the views and the call entry, in light and dark, desktop and phone widths
