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

As built (Phase 2): a table of their own, `project_remote_members`, keyed by address with the server half stored, beside `project_members` — which requires a local account, as `remote_members` is beside `room_members` for rooms. The member routes take `name@server` wherever they take a username; this server's own name is somebody here. `RemoteAccess` resolves a remote member's access with no permissions of its own, so `Allows` answers from the role alone and refuses exactly as it would refuse a local member with that role. Only administrators here count towards a project's last administrator, since nobody elsewhere can act on it until Phase 4. A remote member is the projector's ordinary member, keyed by address, so their changes need no new event types.

The switch is `projects.federated`, not the project room's `rooms.federated`: that flag opens a room to the room federation endpoints, and a project's stream must leave by the project's rules alone. Opening is `project.federation_changed` in the stream and `project.federation.changed` in the audit log; it is refused on a server that does not federate, closing never is, and a project closed again keeps its remote members, inert. Adding somebody from a server the trust settings refuse is refused before it is recorded.

### Who receives a project

- **Not opened to federation:** nobody. The default, as with rooms.
- **Public and opened:** listed to trusted peers in the same browsing a peer's public rooms appear in; any trusted peer may hold a replica (read-only for its people until they are made members).
- **Private and opened:** only servers hosting a current member. Adding the first member on a server starts that server's replica; removing the last stops delivery and tells that server to discard it. A peer that hosts no member learns nothing, including that the project exists; the room-federation test that caught "a peer with no part gets nothing" passing vacuously applies again — the test project must have peers.

### As built (Phase 3)

- **Delivery runs by itself.** The room federation's retry and abandonment bookkeeping existed, but delivery only ever happened when an operator called the deliver endpoint. A loop now delivers every two seconds, claiming peers under row locks so nodes sharing a database never send the same batch twice, only for federated rooms this server is home to (a replica keeps a peer row for its home and must never push the home's events back). Project rooms go first in a pass, since a replica builds the discussion rooms its issues' events are delivered into.
- **Offer, then deliver; discard, then forget.** `project_replicas` on the home lists which servers hold a copy. A set-based sync each pass derives it from `project_remote_members` of opened projects this server is home to: a new server is offered the project (`POST /api/federation/projects/offer`, naming its members there — refused unless one of them exists there), and only once it accepts is it a peer of the project room and every discussion room. A server no longer entitled stops receiving at once and is told to discard (`POST /api/federation/projects/discard`), retried until it answers.
- **Same identifiers everywhere.** A replica adopts the project room and each discussion room under the home's identifiers, so events name the same room on every server and issue identifiers are row identifiers on both. Adoption never touches a room of the receiver's own or another home's.
- **Keys name the home.** A replica's key is `WEB@a.example` (the key check allows the suffix; a project started here may not use it), so it cannot collide with a local `WEB`, and every read path that builds identifiers from the key produces `WEB@a.example-12` unchanged.
- **Rows from the stream alone.** On each arrival the replica replays its stream from the latest baseline and syncs its tables to the result: definitions and issues upserted, what hangs from issues cleared and rewritten, definitions nothing uses removed last, so foreign keys hold throughout. People of the replica's server are members, authors and assignees by account; everybody else by address (`project_remote_members`, `issues.author_address`, `issue_remote_assignees`). Attribution columns (who added, created, assigned) name only local people. An unknown event stops the replica with `projects.replica_error`, rows untouched.
- **Read-only by construction.** `Access.Allows` refuses every permission on a replica, naming the home, and the author exceptions do not apply there; the project response carries `home` and `replicaError`. Posting into a replica's discussion goes to the home's remote-post path, which refuses until Phase 4.3 admits replica members to discussions.
- **Reaching a server.** An address names a host. The home reaches it at the address a room peer records, else an allowlist entry written as a full address, else `https://host`.
- **Not yet** (as of Phase 3; see Phases 4.5 and 5 below for what followed): a replica rebuilds from the whole stream (since the latest baseline) per delivery.

### As built (Phase 4)

- **One endpoint, named changes.** `POST /api/federation/projects/change` carries a `Change` — file, edit, delete, move, (un)assign, (un)label, priority, (un)relate, cycle, milestone — naming issues by number. The home checks the signing server speaks for the actor, holds an accepted replica, and then calls `Service.Apply`, which dispatches to the same methods a local request uses with the access `RemoteAccess` gives, so every rule is the same rule.
- **Credit without impersonation.** The home cannot sign an event in a remote person's name (receivers refuse an event whose sender's server did not sign it). It records the change as itself — bare server name as sender — with `"by"` in the content. Receivers accept a bare sender only from that server. The projector reads an issue's author from `by`; other attribution columns (`*_by`) record only the home's own people, on the home as on replicas, which keeps projection and tables equal. History shows `by`.
- **Synchronous for the person.** The replica's route forwards the change, then catches the replica up from the home before answering, so the response is the issue as it now is — the request in flight is the pending state — and a refusal comes back in the home's words with its status. Abilities on a replica are what the person's role would allow at home (`Access.AsAtHome`), with project administration left to the home.
- **Comments.** Rooms gained `DelegateRemoteMembership`: an issue's discussion admits, on the home, anybody `RemoteAccess` admits to its project, so a comment posted from a replica through the remote-message path is accepted from exactly the people whose changes are. Replica discussion rooms inherit the project room's peer, so they know where their home is.
- **Notifications through the person's own server.** Arriving messages are indexed and notified as local ones; an arriving `issue.assigned` naming somebody of the receiving server notifies them there. The home notifies only its own people.

### As built (Phases 4.5 and 5)

- **Following is "adding" to people.** The protocol says follow; the interface says **Add from another server** and **Remove from this server**, since what happens is that this server keeps a read-only copy for everybody on it. `project_replicas.followed` keeps a follower's copy while the project is public and opened; members entitle it otherwise. The follower adopts the project room when the home agrees, which is what lets it accept an offer naming none of its people.
- **Asking to join, without a notification.** Notifications point at an event, and a join request is not one: putting it in the stream would stop every replica on an older server. `project_join_requests` holds it on the home; the project response carries the count for administrators, shown in its header, and settings lets them in (an ordinary member added) or declines.
- **Keeping up.** Each replica catches up from its home once a minute; that is also the reachability check (`projects.home_unreachable_since`, from the first failure, also set by a change that could not reach the home), and the point where a replica stopped by an unknown event is replayed again. `tdc-server -rebuild-replica KEY` does the same by hand.
- **Path parameters decoded.** The router now unescapes every path parameter: the web client percent-encodes `@`, and `name%40server` had been arriving as a local username, so adding a member from elsewhere through the interface never worked. The event-identifier workaround that did the same for `$` went with it.
- **Delivery order kept.** `UPDATE … RETURNING` returns claimed peers in no order, so a discussion room could be delivered before the project room its replica builds it from; a pass now sorts project rooms first after claiming.

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
