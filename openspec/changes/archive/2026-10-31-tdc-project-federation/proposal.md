## Why

Rooms cross between servers; the work agreed in them does not. A chapter in Arusha and a company in Dar es Salaam can share a channel today, talk about a bug in it, and then have nowhere shared to track the fix: a project lives on one server and its issues, their discussions and its board are invisible to everybody on another. The community the platform exists for is several organisations working together, and the place where they would most need to work together is the one place federation stops.

Projects were built so this would not need a rewrite. A project is already a stream of signed, ordered events in a room of its own, and every issue's discussion is already a room — the two things federation already knows how to share. What is missing is the rest of the arrangement: a project's rows rebuilt from its stream on a server that only holds a copy, changes made by people on that server reaching the project's home and coming back in its order, and the rules for who may see a project from somewhere else.

## What Changes

A project, its issues and their discussions can be shared with other servers, under the same model rooms use: the project's **home server is authoritative** for its membership, its permissions and the order of everything that happens in it; other servers hold a **replica** they rebuild from the project's event stream and never write to directly.

- **Visibility decides who may look, federation decides which servers carry it.** A *public* project may be listed to trusted peers and followed by anybody on them, read-only unless made a member. A *private* project is shared only with servers hosting one of its members, and nothing about it — not its name, not that it exists — reaches any other server. Neither is shared at all until somebody allowed to administer the project opens it to federation, exactly as a room is.
- **Remote members.** A person on another server can be made a member of a project, with a role, and is then held to the project's permissions exactly as a local member is — enforced by the home server, on every change.
- **Changes from anywhere, ordered at home.** Filing, editing, moving, assigning, labelling, planning and commenting on an issue from a replica goes to the home server as a signed request in the author's name; the home server checks it, records it in the project's stream, and it reaches every replica in that order. A replica never allocates a position in a project's stream, the rule rooms already keep.
- **Replicas rebuild, they do not copy tables.** A replica's projects, issues, states, labels, priorities, cycles, milestones, assignments and relations are a projection of the stream, rebuilt deterministically from the events. Anything a project records that is not yet an event becomes one, so the stream is complete.
- **Discussions are rooms already.** An issue's discussion federates as the room it is, with the room rules unchanged; comments from remote members go the way remote messages already go.
- **Boards, search and notifications on a replica.** The board, issue lists, saved views and search work over the replica for local readers, under the project's permissions as the home server states them. Being assigned or mentioned from another server notifies a person through their own server.
- **What never crosses.** Saved views, notification settings and drafts stay local. A directory engagement linked to a project is never shared with it — the link says where work is tracked, never the terms.

## Capabilities

### New Capabilities

None. This extends `projects/boards`, `projects/issues` and `federation/protocol`.

### Modified Capabilities

- `federation/protocol`: gains project sharing — what a project replica is, who may hold one, and how changes from a replica reach the home server.
- `projects/boards` and `projects/issues`: gain remote membership and the guarantee that a replica's view of a project is exactly what its stream says.

## Impact

- **Data model**: projects and the tables hanging from them gain the home server they belong to; project membership gains remote people; a replica's rows are marked as such and are written only by the projector. No existing column changes meaning for a project that is not shared.
- **Event log**: events for every project change not yet recorded as one (state, label and priority configuration, cycle and milestone changes, membership), so a replica can be rebuilt from the stream alone.
- **Security surface**: a second kind of thing accepted from other servers. Every change from a replica is a signed request the home server authorises against its own record of the project's members; a replica cannot grant itself anything, and a forged or out-of-order event is refused rather than partly applied.
- **Operators**: a project-level switch beside the room-level one, and the same operational reality — a shared project is read-only on replicas while its home server is unreachable.
- **Out of scope**: moving a project's home from one server to another; attachments on issues from replicas (rooms from replicas cannot carry attachments yet either, and this change does not solve that separately); cross-server search beyond the projects a server holds.
