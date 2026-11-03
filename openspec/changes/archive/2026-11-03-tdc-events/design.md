## Context

Rooms, their logs, notifications, push, digests, webhooks and calls all exist, and all of them are keyed on a room: a notification points at a room and an event in its log; a webhook subscribes to a room; a call belongs to a room. Issues already show the pattern of "a thing that is a room too": an issue's discussion is a room of kind `issue-discussion` whose id is the issue's, so comments, notifications and mentions came for nothing. Time is stored as `TIMESTAMPTZ` everywhere and shown through date-fns with an explicit zone. The server already fetches addresses people give it — link previews and webhooks — through a connect-time guard that refuses private and reserved addresses.

## Decisions

### An event is a room too

Every event has a room of kind `event`, private, whose id is the event's. The room is the event's conversation: its log carries what happened to the event (`event.created`, `event.updated`, `event.cancelled`, `event.rsvp`, `event.reminder`), its members are the event's hosts, speakers and the people going or maybe going, and its call is the event's call.

That one decision buys most of the feature from what exists:

- **Notifications** point at the event room and an entry in its log, as every notification points at a room and an event; muting the event's conversation silences its reminders, through the preference check every notification already passes.
- **Calls** are the event room's call. Nothing about calls is reimplemented: the room is call-capable, attendees are members, and the existing call routes, signalling, relay credentials, links and lobby all apply. The only addition is that in an event's room only a host (or somebody holding `event.manage.any`) may *start* the call; anybody in the room may join it once started.
- **Webhooks** for the channel an event belongs to receive the event room's calendar entries, forwarded at queueing time (below).

Membership is managed by the calendar, never by people: the room is private, so nobody can join it, and issuing room invitations for it is refused. An answer of going or maybe adds somebody; not going, or being put on the waiting list, removes them (a host stays). Room lists, search and channel counts exclude `event` rooms as they exclude issue discussions, and the web client sends somebody who opens one — from a notification, say — to the event's page.

*Why not hold the call in the channel?* A server-wide event has no channel, and an invite-only event in a channel must not admit every channel member to its call. One rule for every event is simpler than two.

### What an event holds

`calendar_events`: kind (`meetup`, `talk`, `workshop`, `office_hours`, `other`), title, description (Markdown, rendered by the message renderer), `starts_at`/`ends_at` (instants), `time_zone` (an IANA name), `all_day`, `recurrence` (an RRULE body), `exceptions` (skipped occurrence starts), `location` (an address in words), `online` (hosted in TDC) and `online_url` (somewhere else), `capacity`, `visibility`, `channel_id`, `cover_attachment_id`, `uid` and `sequence` for iCalendar, `subscription_id` for events that came from a subscribed calendar, `cancelled_at` and its reason, and `last_ends_at` — the end of the last occurrence, null for a series without end — which is what a range query filters on before occurrences are expanded.

Hosts and speakers are `calendar_event_people` (one role per person per event). The agenda is `calendar_sessions`: a title, a description, a start and end within the event, speakers on the server and, in words, speakers who are not.

### Time

Instants are stored in UTC (`TIMESTAMPTZ`), the zone beside them. The zone matters for two things only: expanding a repeat (a weekly 18:00 meetup stays at 18:00 local time across a daylight-saving change, because the rule is expanded in the event's zone) and saying what the organiser meant. Clients show times in the reader's zone and, whenever the event's zone differs, the event's zone as well ("18:00 EAT · 16:00 your time"). An all-day event is midnight to midnight in its zone and is exported as dates, never as instants.

This machine runs EAT against a UTC database, so the tests that matter run in both: a weekly series created in Africa/Dar_es_Salaam and one in Europe/London across the October change are expanded and checked against wall-clock times.

### Repeats

A subset of RRULE, enough for community life: `FREQ` of `DAILY`, `WEEKLY` or `MONTHLY`, with `INTERVAL`, `COUNT` (at most 500) or `UNTIL`, `BYDAY` (with an ordinal for monthly "second Tuesday") and `BYMONTHDAY`. Anything else is refused when written and reported, when imported, as imported without its repeat. Exceptions are occurrence starts to skip. Expansion uses rrule-go in the event's zone. An answer covers the whole series; reminders are per occurrence.

### Visibility

Decided in one function, used by every route, the feeds and the reminder pass:

- a creator, host or speaker sees their event;
- somebody holding `event.manage.any` (at the event's channel, or at server scope for a server-wide event) sees it;
- *public*: everybody signed in to the server. A public event may belong only to a public channel, so it cannot disclose a private channel's name;
- *channel*: members of the channel who may read it;
- *invite*: people invited directly, members of an invited channel, and anybody who has already answered.

An event somebody cannot see is answered exactly as one that does not exist.

### Permissions

- `event.create`, server and channel scope. A server-wide event takes it at server scope; a channel's event takes it in that channel, and the creator must be able to read the channel. Granted to Member on new servers and by the migration on existing ones: planning a meetup is what an ordinary member of a developer community does.
- `event.manage.any`, server and channel scope: edit, cancel, delete and manage invitations of anybody's events, and see every attendee. Nobody by default.
- `event.calendars.manage`, server and channel scope: subscribe the server, or a channel, to an external calendar. Nobody by default, because it makes the server fetch an address on a schedule.

An event's creator and its hosts may edit it, invite to it and cancel it; deleting it takes its creator or `event.manage.any`. Cancelling, deleting, acting under `event.manage.any` on somebody else's event, and adding or removing a subscription are audited, as are creating and revoking a feed address.

### Answers, capacity and the waiting list

`calendar_rsvps` holds going, maybe, declined and waitlisted, with when. An answer is written in a transaction holding the event row, so two people taking the last place cannot both have it. "Going" to a full event is "waitlisted"; when a place frees — somebody changes their answer, or the capacity rises — the earliest on the waiting list becomes going and is told. Hosts and speakers do not count against capacity.

### Who is shown as going

A new per-person privacy choice, `showsEventAttendance` (on by default, beside the other disclosure settings on `user_privacy`). An attendee list shows the people who disclose, counts the rest, and shows everybody to hosts and to `event.manage.any`. The `event.rsvp` log entry, and so its webhook, is written only for somebody who discloses.

### Invitations

`calendar_invites` names a person or a channel. Inviting is for hosts and managers; an invited person is notified (`event_invite`), and an invited channel's members are notified when they would not otherwise have seen the event. An invitation is what makes an invite-only event visible.

### Reminders

A pass every minute on every node. Lead times are the server's `events.reminder_minutes` (default `1440,15` — the day before and a quarter of an hour before). For each occurrence starting within a lead time, a row in `calendar_reminders` (event, occurrence, lead) is claimed with an insert that only one node can win; the winner appends `event.reminder` to the event's log and tells each person going, each host and speaker, and each person answering maybe:

- a notification (`event_reminder`), which appears in the application and, for people who chose summaries, in their next email digest;
- a web push to their subscribed browsers;
- an `event.reminder` live frame, which the web client shows and the desktop application turns into a banner when its window is hidden.

Muting the event's conversation silences them. A reminder is never sent for an occurrence already started, so a server that was down does not remind people of something past.

Changes that matter to attendees — the time, the place, cancellation — notify them too (`event_changed`, `event_cancelled`), and somebody moved up from the waiting list is told (`event_promoted`).

### Webhooks

The four types are added to the catalogue a subscription may ask for. They are stored in the event's room; when queued, an entry in the room of an event that belongs to a channel is also offered to that channel's subscriptions, with the subscription owner's ability to read the channel checked as for every delivery. Invite-only events are never forwarded, because a channel reader may not be somebody the event was shown to. Server-wide events have no channel and no webhooks.

### Export and feeds

- `GET /api/events/:id/ics` — one event, for somebody who can see it.
- A personal feed: `POST /api/me/calendar-feed` makes (or replaces) an address with a secret in it, shown once; `DELETE` revokes it. The secret is stored as a SHA-256 hash, like call links. `GET /api/calendar/feeds/:secret/events.ics` serves, with no session, the events its owner is going to, hosting or speaking at, plus the events of their channels that they can see — visibility decided for the owner at every fetch, so leaving a channel or being suspended removes its events from the feed at once.
- A public feed: `GET /api/calendar/public.ics` of the server's public events, only while `events.public_feed` is on (off by default: "public" means public on the server, and publishing it to the internet is the operator's decision).

The writer produces `VCALENDAR` with `PRODID`, `METHOD:PUBLISH`, `X-WR-CALNAME`, a `VTIMEZONE` for every zone a repeating event uses (derived from the zone's current rules: one `STANDARD` for zones without daylight saving, a `STANDARD`/`DAYLIGHT` pair with month-and-weekday rules otherwise), `DTSTART;TZID=` for repeating events and UTC for single ones, `RRULE`, `EXDATE`, `STATUS:CANCELLED`, `SEQUENCE`, `URL` back to the event page, and `LOCATION`.

*Why not OAuth to Google and Microsoft?* Every operator would have to register an application with each, keep its secret, pass each provider's verification for calendar scopes, and the server would hold refresh tokens to people's calendars. A subscription address does the useful half — events appear and update in the calendar people use — with none of that, and works with Apple Calendar, Thunderbird and everything else too. Each event also offers "Add to Google Calendar" and "Add to Outlook" links, which open the provider's own form with the event filled in; nothing is sent to them by the server. The user guide gives the exact steps for each.

### Import

`POST /api/events/import/preview` takes an `.ics` file (at most 1 MB) and says, for each `VEVENT`, what importing would do: create, update (same UID, an event the person may edit), or skip (same UID, somebody else's), with warnings — a repeat outside the subset, a zone that could not be resolved. `POST /api/events/import` takes the same file, the UIDs chosen, and the channel, visibility and kind to give them. Time zones are resolved by `TZID` as an IANA name, then through a table of the Windows zone names Outlook writes, then from the file's own `VTIMEZONE` when it has a single fixed offset, and otherwise as UTC with a warning. `DATE` values are all-day events. `RRULE` and `EXDATE` carry over when they fit the subset. Imported events are ordinary events whose creator is the importer.

### Subscriptions to external calendars

`calendar_subscriptions`: an address (`webcal://` read as `https://`), a label, a channel or none, the visibility and kind to give its events, and fetch state (`ETag`, `Last-Modified`, last error, failures). Refreshed every `events.subscription_refresh_minutes` (default 60) by whichever node claims it, and on demand. Each refresh upserts by UID within the subscription and cancels events that have disappeared from it. Fetching goes through the same guard as webhooks: the address is checked when the subscription is made, every connection — including after a redirect, at most three — is judged after DNS resolution and immediately before connecting, private and reserved addresses are refused unless `integrations.allow_private_addresses` is on, the response is capped at 2 MB and 20 seconds, and no proxy from the environment is used. A subscription failing ten times in a row is paused and its owner told.

### Client

`useEvents` holds the API. Pages, all in the shared layer and loaded only when visited: `/events` (agenda, month and week, upcoming and past, with a channel filter), `/events/:id` (the event: when in both zones, where, the call, the agenda, hosts and speakers, attendees, the answer buttons, invitations, export), and the create/edit form as a dialog loaded on demand (TanStack Form and Valibot). A channel shows its next events in its header. Personal settings gain the calendar feed (make, copy, revoke, with how to add it to each calendar). Import and subscriptions live behind a menu on the Events page. Motion follows `data-motion`.

The desktop application shares all of it, and its live bridge raises a banner for `event.reminder` frames as it does for rings.

## Risks / Trade-offs

- **One answer per series.** Somebody going to a weekly office hour is going to all of them; skipping one week is not expressible. Recorded; per-occurrence answers can be added without changing the series model.
- **Notification reasons are a CHECK constraint** that other changes also redefine. Whichever migration runs last must list every reason; the migration here lists all known ones.
- **Time-zone fidelity on import** is best-effort for zones that are neither IANA nor known Windows names; the preview says so for each event rather than guessing silently.
- **A feed address is a bearer secret.** Anybody holding it reads its owner's calendar. It is shown once, revocable, and its fetches are counted so an owner can see it is in use.
- **Subscriptions are an outbound request on a schedule.** Bounded as above, and behind a permission nobody holds by default.
