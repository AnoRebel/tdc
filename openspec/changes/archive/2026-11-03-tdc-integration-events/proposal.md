## Why

Outgoing webhooks could listen to one channel and nothing else, so a tracker bridge, a welcome bot or an assistant following a project had nothing to subscribe to: issues, milestones, a project's members, new accounts and new channels were all invisible to programs. And two of the twelve offered types — joins and leaves — were never actually sent, because the paths that store them publish nothing live.

## What Changes

- **Three scopes.** A subscription listens to a channel, a project, or the whole server. A server subscription hears an event in any room its owner can read at that moment, and nothing from the rest.
- **A described catalogue.** `GET /api/webhooks/event-types` gives each type's group, the scopes it can be asked for in, the permission it needs and whether the caller could ask for it now. Types are added for calls, directory engagements, channel creation, every project and issue event in a project's stream, cycles and milestones, and three the server names by reading an event: `issue.commented`, `issue.closed`, `issue.reopened`.
- **Events in no room.** `account.created` and `account.role_changed`, for whoever holds `user.read.all`, never with an email address.
- **One place every event is queued.** The rooms service and the projects service tell webhooks about every event they store, after it is committed — which is what makes joins, leaves and channel creation arrive.
- **Applications subscribe too**, through the same endpoint, held to their grant: their own channels, projects they were made members of, and permissions they were granted.
- **Settings → Integrations** lists the types grouped and described, in English and Swahili, and offers the three scopes.

Payload version 1 gains optional `project` and `issue` fields; `room` is absent and `event.sequence` omitted only for an event in no room. Nothing a version 1 receiver relied on changes.

**Out of scope:** reactions, pins, directory agreements and server invitations, which are not events in any log; calendar events, which another change adds as a group of their own; delivering project and server events over the live connection.

## Capabilities

### Modified Capabilities

- `integrations/webhooks`: subscription scopes, the event catalogue, events in no room.
- `integrations/apps`: applications subscribe to events.

## Impact

- **Database:** migration 000427 makes `webhook_subscriptions.room_id` nullable and adds `scope`.
- **Security:** every event is checked against what its subscriber can read when queued and again when sent; a private project is refused as not found; the people events need `user.read.all`.
