## Context

Two facts about how projects were built make this tractable.

1. **A project is a room.** Its row shares an identifier with a room of kind `project`, and every change to every issue in it is a signed event appended to that room in the same transaction that changes the issue's columns (`server/internal/projects/stream.go`). `appendSigned` already refuses to append to a room another server orders — the guard written for the day projects federated.
2. **An issue's discussion is a room** of kind `issue-discussion`, sharing the issue's identifier, holding comments as ordinary messages.

Federation already replicates rooms: home-server ordering, remote authorship verified against the author's server's keys, delivery with cursors, catch-up, abandonment, local removal. So the shape of this change is: replicate a project's room and its issues' rooms by the existing means, make a replica rebuild the project's tables from the stream, and route changes from replicas to the home server.

## Goals / Non-Goals

**Goals:** share public and private projects with the right servers and nobody else; remote members held to the project's permissions; a replica that is exactly what its stream says; the board, lists, views and search working on replicas for local readers.

**Non-goals:** moving a project between homes; resolving concurrent edits other than by the home server's order; cross-server search; sharing saved views, drafts or directory engagements.

## Decisions

### The stream is the source; tables on a replica are a projection

On the home server nothing changes: the service writes the row and the event together. On a replica, rows are written only by a projector that applies events in sequence order: `issue.created` inserts an issue, `issue.state_changed` moves it, and so on. The projector is a pure function of (current rows, next event) → rows, so a replica that replays its stream from the start arrives at the same rows as one that followed it live — which is also how its correctness is tested: replay the home server's stream into an empty database and compare.

*Why not copy rows?* Rows have no signature and no order. A replica holding copied rows could not show that what it holds is what the home server said, and two replicas could disagree with no way to tell which is right. Events carry both.

Corollary: **every change becomes an event.** An audit of every write path in `projects` finds those that change rows without appending (expected: state, label and priority configuration; cycles and milestones; membership; project rename and visibility). Each gains an event type carrying everything the projector needs. This is the bulk of Phase 1 and is valuable on its own: a project's history becomes complete.

### What the audit found

Read against the code (Phase 1), the write paths in `projects` that changed rows without recording enough were fewer than expected, and of a different kind. Membership, rename and visibility, states, cycles and milestones already appended events. What did not:

- **Labels**: adding, renaming or recolouring, and removing a label wrote no event (removing one did record each issue losing it, but not the label going).
- **The priority scale**: the four levels every project starts with, adding, renaming, reordering and removing a level — none recorded.
- **Incomplete content** where an event existed: `project.created` lacked the description, calendar and cycle policy; milestone events lacked the description; `project.cycle_ended` lacked the instant the cycle was closed.
- **The database's own cascades**: removing a cycle or milestone silently takes a *deleted* issue out of it. Not an event, and not needed as one — the projector applies the same cascade.

Each now has an event (`project.label_added|changed|removed`, `project.priority_added|changed|removed`) or the missing fields, appended in the same transaction. Outside the package, no handler writes a project table directly. Saved views, discussion watching, admitting a mentioned person to a discussion room and the search index are deliberately not project rows. The guard is a test that walks every exported method of the service: each is either exercised by the replay scenario — which checks that a step changing rows grew the stream and that the projection still equals the tables — or listed with the reason it is not a write.

"Rows" means the columns the stream decides. When a row was written (`created_at`, `joined_at`, `assigned_at`, …) is not among them: it is when that database wrote it, which a replica answers differently and correctly. People are compared by address.

### An existing project's stream is completed by one baseline

A project made before this change has a history that leaves out its labels and priority scale. Replaying it cannot reproduce the project, and the missing events cannot be invented after the fact with honest timestamps or authors.

So such a project is given one **`project.baseline`** event, appended by the server and signed by it, whose content is every row the project holds at that moment. A replay starts from the latest baseline; everything before it is already in it. `projects.stream_complete` records which projects have one or never needed one: existing rows start `false`, new projects `true`. At startup the server appends a baseline for each incomplete project, each in its own transaction holding the project's stream lock, so a concurrent change lands either inside the baseline or after it, and marking the row first means two servers sharing a database append one between them.

*Why not a migration?* A migration cannot sign: the keyring belongs to the running server. *Why not synthesise the missing events?* They would claim a person did something at a time nobody knows.

### An account's deletion is followed by a baseline

Erasing a person empties the content of every event they sent, project events included, and deleting their account cascades their project memberships and assignments with no event. An erased person's `issue.created` cannot be applied, so the stream stops describing the project — exactly what `stream_complete` already says, so the same remedy applies. A `BEFORE DELETE` trigger on `users` marks incomplete every project the person sent an event in, belonged to or was assigned in, and the server's once-a-minute maintenance pass (the one that closes ended cycles) appends a baseline to each. Replay starts from the latest baseline, so the emptied events are never needed again; the progress trend also restarts its count at each baseline and passes over emptied events.

*Why a trigger?* Accounts are deleted by hand against the database too — that is how an operator carries out an erasure — and Go never sees that. *Why not keep project events' structural fields through erasure?* It would make "the content of everything you sent is removed" untrue for one kind of event, and a baseline gets the same result without that exception.

What remains: events other people sent still name the erased person by address — an `issue.assigned`, a `project.member_added`, an earlier baseline — as other people's messages that mention them do. That is the boundary erasure already documents.

### Changes from a replica are requests to the home server

A person on a replica who files or moves an issue does not change any local row. Their server sends the home server a signed federated request naming the author (`user@server`) and the change; the home server authorises it against **its** membership and permission records — the same `Access` and permission checks a local request passes — applies it, appends the event, and replicates it. The replica's interface shows the change when the event arrives back; a short pending state covers the round trip, as remote messages already do.

The request is signed by the author's server and its author must belong to that server (`origin == ServerOf(author)`), the two-checks rule from room federation. A replica cannot act for somebody it does not host.

### Membership with remote people

`project_members` gains remote members by address, alongside local ones by account. A remote member's role is held by the home server; the replica shows it but cannot change it. The baseline rule (`Member` role overridable per scope) applies unchanged to remote members' server-level grants on *their own* server and not at all to the project's home: a project's permissions for a remote member come only from their project role.

### Who receives a project

- **Not opened to federation:** nobody. The default, as with rooms.
- **Public and opened:** listed to trusted peers in the same browsing a peer's public rooms appear in; any trusted peer may hold a replica (read-only for its people until they are made members).
- **Private and opened:** only servers hosting a current member. Adding the first member on a server starts that server's replica; removing the last stops delivery and tells that server to discard it. A peer that hosts no member learns nothing, including that the project exists; the room-federation test that caught "a peer with no part gets nothing" passing vacuously applies again — the test project must have peers.

### Issue discussions

Each issue's discussion room is federated together with its project, to the same servers, with the room rules unchanged. Comments from replicas use the existing remote-message path. Creating an issue on the home server creates its discussion room federated the same way.

### What a replica's reader can do locally

Board, lists, saved views, search and notifications run against the replica's projection. Views stay local. Search indexes replica issues for local readers. Notifications: an `issue.assigned` or a mention naming a local person produces a local notification on that person's server when the event arrives — nothing is sent to notify them separately.

### Directory engagements

Never federated. The engagement→project link is a column on the engagement on the server that holds the engagement, and nothing in the project stream refers to it.

## Risks / Trade-offs

- **Projector drift** — a projector bug makes a replica disagree with home. Mitigation: the replay-equals-home test across every event type, run in CI, and a per-project "rebuild from stream" operator command.
- **Home unreachable** — replicas are read-only. Accepted, as for rooms, and shown in the interface.
- **Stream growth** — every configuration change is now an event. Acceptable: these are rare compared with messages.
- **Version skew** — an older replica meeting an event type it does not know must stop applying that project's stream (not skip the event) and say so, because skipping would silently diverge. The protocol version gains the project event family.

## Migration Plan

Additive. Existing projects are not shared until opened. Phase 1 (events for every change) ships before anything federates and changes no behaviour except fuller history — and a project history page showing it — plus one baseline event per existing project, appended at the first start on the new version.
