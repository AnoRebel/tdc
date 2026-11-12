## Context

See `proposal.md` for motivation. This document records the technical decisions for implementing `projects/issues` and `projects/boards` against the platform built in the foundation change.

Two constraints carry over unchanged from the foundation design and are not re-argued here:

1. **Zero-cost self-hosting.** No new dependency, paid or AGPL-default, of any kind.
2. **Mobile-first on metered data.** A board or list view is a paginated query, never a full-project dump.

A third constraint is specific to this change: **issues must not become a second product bolted next to chat.** Every capability that chat already solves — permissions, visibility, discussion, notification, search, history — is reused rather than rebuilt. What is genuinely new is the tracking surface itself: states, assignment, labels, relationships, boards.

## Goals / Non-Goals

**Goals:**

- A message becomes an issue, and an issue remembers the conversation that produced it, without either side leaking what the other party cannot see.
- Issue history is the event log, not a parallel audit table.
- Issue discussion is channel messages, not a parallel comment system.
- Issue search is the existing `Searcher` interface and the existing Postgres FTS machinery, not a second index.
- A board is usable on a mid-range phone on metered data.
- Every phase ends in something a person can use, per the house rule established in the foundation change.

**Non-Goals** (see `proposal.md` for the full scope-creep list):

- Workflow automation, story points, custom issue types, Gantt/critical-path scheduling, time tracking.
- Federation of projects across servers. Projects live on one home server, same as rooms.
- Any UI concept a Jira user would recognise as "workflow configuration as code."

## Decisions

### Linking conversation to work: the issue's discussion room *is* a channel, and the origin reference is an event

This is the seam the proposal calls the reason to build this here, so it gets the most scrutiny.

**Decision.** Creating an issue does two things atomically:

1. Creates the issue row (project, key-derived identifier, title, initial state, author) and appends an `issue.created` event carrying that content to a **project-scoped event stream** (see "Issue history as events" below).
2. Creates a normal room of kind `issue-discussion`, owned by the project, with room ID equal to the issue's ID. Issue comments are ordinary `message.sent` events in that room — the same pipeline, same formatting, same attachment limits, same mention/notification behaviour as any channel, because it *is* a channel in every mechanical sense. `projects/issues`' "Discussion" requirement is satisfied by not building anything new.

When an issue is created **from a message** (the "linking conversation to work" requirement), a third thing happens:

3. The originating message's channel and event ID are recorded on the issue as `originChannel` and `originEvent` fields in the `issue.created` event content — plain references, not a copy of the message body. The origin channel gets a lightweight `message.annotated` event (or a room-scoped side table, see below) marking that message as "issue created from this," so the channel shows the back-reference without duplicating content.

**Why a reference and not a copy.** Copying the message text into the issue would let it outlive the channel's own retention policy, redaction, or a later deletion — exactly the kind of drift the event log's redaction design (foundation `design.md`, event redaction) exists to prevent. A reference stays honest: if the message is later redacted, the issue's reference resolves to whatever the redacted message now shows, which is the same non-disclosure guarantee `platform/audit-log` already gives.

**Why permission-respecting rather than permission-copying.** The spec is explicit: *"a user without access to the originating channel views the issue [and] the reference does not disclose the message content."* The issue never stores channel membership or a snapshot of the message; it stores a pointer. Resolving that pointer at read time means:

- A viewer who **is** a member of the origin channel sees the message content (or an "edited"/"deleted" marker, same as they would in the channel itself).
- A viewer who **is not** a member sees that the issue was created from a message, in a named channel if the channel's own visibility allows naming it, or "a private conversation" if it does not — but never the content, never a way to infer the content by timing or metadata.

This is exactly `Visible`/`RequireMembership` from `server/internal/rooms/`, reused rather than reinvented: resolving the origin reference calls the same membership check a normal message read would.

**Why this is not a second reference direction to build.** The issue's `originChannel`/`originEvent` are the only new pointer. The *message* side of the link ("the message indicates that an issue was created from it," per the spec) does not need its own storage: it is answered by an indexed lookup of `issue.created` events whose content references that message, the same way a thread's parent is found today. No new foreign key column on `messages`.

**Turning discussion in a channel into an issue creation form:** the client pre-fills title from the first line (or lets the author type one) and description from the remainder of the message, offering to include the message as the origin reference. This is a client-side convenience over the same create-issue endpoint; the server has no separate "create from message" code path, only the optional `originChannel`/`originEvent` fields on the ordinary create call.

**What is deliberately not built here:** no bidirectional sync (editing the issue does not edit the message; editing the message does not edit the issue), no "issue mirrors this thread's replies," no requirement that the origin message and the issue's discussion room be the same room. They are related by reference, not merged. Keeping them separate is what lets the origin reference respect a different (stricter) permission boundary than the issue's own discussion.

### Issue history: the event log, not a parallel audit table

**Decision.** Every recordable issue change is an event, appended to the existing signed event log with a new family of `Type`s:

```
issue.created
issue.state_changed
issue.assigned
issue.unassigned
issue.labeled
issue.unlabeled
issue.priority_changed
issue.related        (parent/child, blocks/blocked-by)
issue.unrelated
issue.title_changed
issue.description_changed
issue.milestone_changed
issue.cycle_changed
```

Each carries `{field, previousValue, newValue}`-shaped content, or the specific shape a relationship/assignment change needs. Sequence, sender, origin, signature, and content-derived ID all come from the mechanism already built in `server/internal/events/event.go` — nothing about that package changes.

**Why events rather than a dedicated `issue_history` table.** The spec's own requirements make the case: *"History MUST NOT be editable"* and *"showing what changed, who changed it, and when"* is, word for word, what the event log already guarantees for every other kind of state change on the platform. Building a second, append-only, actor-and-time-stamped, tamper-evident log for issues specifically would duplicate exactly the properties `platform/event-log` was built to provide once. It would also fork "what does tamper-evidence mean on this platform" into two answers instead of one, which is the kind of drift the foundation design explicitly tries to avoid (see its own reasoning for putting messages and room state in one log rather than several).

**What is genuinely new**: a room scope for issue events. Today's event log is per-room (`Room string` field, per-room `Sequence`). Two options were considered:

- **(a) Issue events live in the issue's discussion room's stream**, interleaved with `message.sent` events from comments, distinguished by `Type`.
- **(b) Issue events live in a project-scoped stream**, separate from any one room.

**Decision: (b), a project-scoped stream**, for one concrete reason: an issue's state and relationship changes must be readable (subject to project permissions) independently of anyone having ever joined its discussion room, and a project has many issues but the log needs one per-project total order for progress reporting to be computed by scanning forward from a checkpoint rather than fanning out across every issue's room. The discussion room holds only `message.sent`/edit/delete events, same as any channel; the project stream holds only `issue.*` events. An issue's full history for display is the project stream filtered to that issue's ID — cheap, since it is one indexed column, and it keeps the room abstraction meaning exactly what it means everywhere else on the platform: a place people talk, not a place state lives.

**Read-side materialisation.** `Progress reporting` requires figures "derived from recorded state transitions rather than maintained as independent counters that can drift." The issue's current state, assignees, and labels are still stored as ordinary mutable columns on the `issues` table for the sake of every query that is not "show me the history" — a board query filtering six hundred issues by state cannot replay six hundred event streams on every page load. Those columns are written by the same handler that appends the event, in the same transaction, so they can never disagree with the log that justifies them; the log is the source of truth and the columns are its cache, the same relationship the foundation design already uses between the event log and, say, unread counts.

### Data model: projects reuse the room membership shape, issues do not become rooms

**Decision.** A `projects` table (id, key, name, visibility, created_by) with a `project_members` table shaped exactly like `room_members` — same `Visible`/`RequireMembership` pattern from `server/internal/rooms/rooms.go`, copied to a `projects` package rather than generalised through an interface. Generalising membership across rooms and projects into one polymorphic table was considered and rejected: a project's membership answers "can this person see this project's issues," while a room's answers "can this person see this room's messages," and forcing one table to serve both means every future room-only or project-only feature (thread inheritance, issue discussion rooms nested under a project) needs a join to figure out which kind of thing it is looking at. Two small, obviously-parallel tables cost less than one clever one, and match the "some work is deliberately done twice" principle the foundation design already states for the `ApiClient` extraction.

`issues` (id, project_id, key — the human-readable `PROJ-123` identifier — title, description, state_id, author_id, priority, created_at) is its own table, not a room. An issue is not a place people join and leave; its *discussion* is a room (see above), but the issue itself is a row with a stable identifier, matching the spec's explicit requirement that the identifier "does not change" when the issue is renamed, moved, or reassigned — a property rooms do not need to guarantee today and issues must.

**States** are project-scoped rows (`issue_states`: id, project_id, name, position, is_completion) rather than a fixed enum, per the spec's requirement that operators or project administrators configure them. Removing a state in use is refused unless the caller also names where its issues move, satisfying the "Removing a state in use is handled" scenario without a background migration step.

**Relationships** (`issue_relations`: from_issue_id, to_issue_id, kind — `parent`/`child`/`blocks`/`blocked_by`) are stored as directed edges with the inverse kind implied, and cycle prevention is a graph walk bounded by project size (issues number in the hundreds to low thousands for this audience, not millions) rather than a recursive CTE depth limit — simple and provably correct beats clever here.

### Search: one more table shaped like `message_search`, not a second engine

**Decision.** An `issue_search` table mirroring `message_search` exactly — `issue_id`, `project_id`, a generated `tsvector` column over title+description, a GIN index — with `IndexIssue`/`UnindexIssue`/`SearchIssues` queries following the same pattern as `IndexMessage`/`UnindexMessage`/`SearchMessages` in `server/internal/db/queries/search.sql`. The existing `search.Searcher` interface gains an issue-search method (or a sibling interface with the same shape); `server/internal/search/search.go`'s `Service` gets a second small set of methods rather than a second implementation of the interface.

Reindexing after a title or description edit does not touch the `issues` table, for the same reason the foundation design gives for messages: rebuilding an index must never be a write against the row of record. Authorization is the same join-not-filter pattern: `SearchIssues` joins `project_members` the way `SearchMessages` joins `room_members`, so a result from a project the caller cannot see is never fetched, not fetched-then-hidden.

Comments on an issue are ordinary messages in the issue's discussion room, so they are **already** covered by `SearchMessages` — searching issue discussion needs no new machinery at all, only a client affordance that also queries by room.

### Permissions: new entries in the existing catalogue, project-scoped like channel permissions are channel-scoped

**Decision.** `authz.Scope` already distinguishes `ScopeServer` from `ScopeChannel`; this change adds `ScopeProject` alongside it, and a matching set of catalogue entries:

```
project.create
project.administer      (rename, change visibility, manage members)
project.configure_states
issue.create
issue.transition        (move between states)
issue.assign
issue.label
issue.relate
issue.delete
issue.comment           — deliberately absent: commenting on an issue is
                           posting to its discussion room, gated by the
                           existing channel.post-equivalent check on that
                           room, not a new permission
```

Each is declared with `Scopes: []Scope{ScopeProject}` (some, like `issue.transition`, may also be grantable server-wide for an administrator role) following `GrantableAt`'s existing shape exactly — no change to the `authz` package's mechanism, only new catalogue rows and one new `Scope` constant. A route handler asks for a permission by name exactly as `httpapi/adminroutes.go` and friends already do; nothing about *how* a route declares what it needs is new.

### Boards and views: server-side filtering, columns are a client projection of state, never a client-side full-project fetch

**Decision.** A board is not a stored object — it is a list query (`issues where project_id = ? and state in (...)`, paginated per column) plus the project's ordered `issue_states` to know which columns exist and in what order. This is what "Boards ... presenting issues in columns derived from their states" in the spec means literally: the column list is derived, not configured separately from states, so there is exactly one place to add or reorder a state and the board updates itself.

**Payload discipline for metered data**: a board request returns, per column, a bounded page of issue summaries (key, title, assignee avatar reference, priority, label chips — not description, not comments, not history) with a cursor for more. A project with six states and eight hundred issues never ships eight hundred issues to draw six columns; it ships the visible page of each column, same principle as the foundation design's payload-budget CI check for the chat client. Moving a card requests one state transition, not a board re-fetch of everything — the client patches the one issue's column locally and reconciles from the server's response.

**Saved views** (`project_views`: id, project_id, owner_id, name, filter (JSONB), sort, shared boolean) store the filter/sort configuration, not a result set. Opening a shared view re-runs the query under the *viewer's* permissions — satisfying "Shared view respects the viewer's permissions" by construction, since the query already joins project membership the way search does, rather than needing a second check bolted on afterward.

**Concurrent board moves converging deterministically**: a state transition is a single `UPDATE ... WHERE id = ? AND state_id = ?`-style compare-and-set inside the same transaction that appends the `issue.state_changed` event, keyed on the issue row, not on a board's view of it. Two simultaneous moves serialize at the database; the second writer's precondition fails against the first writer's already-applied state, so it is retried against the new state rather than silently overwriting it. Both clients converge because both are told the actual resulting state, not their own optimistic guess.

### Cycles, milestones, progress: read-side aggregates over the same state and event data, no new counters

**Decision.** `cycles` (id, project_id, name, starts_at, ends_at, carry_forward_policy) and `milestones` (id, project_id, name, target_date) are thin grouping tables; `issue_cycle`/`issue_milestone` are join tables (an issue can move between cycles, and milestones span cycles per the spec). Neither stores a completion count. Progress for a project, cycle, or milestone is computed at read time as `count(*) where state.is_completion` over the relevant issue set — cheap at this scale (hundreds of issues, not millions) and, per the spec's explicit requirement, provably non-drifting because there is no counter to drift: recomputing it from currently-stored state (itself kept correct by the same transaction that appends the event, as above) always matches what is reported.

**Chart rendering** reuses the chart-adapter interface the foundation design already established for TanStack Charts with a documented fallback — no new charting dependency, and the "Charts degrade gracefully" scenario is satisfied by the same "figures remain available in a readable form" fallback the adapter interface already requires of its callers.

**Cycle end with incomplete issues**: `carry_forward_policy` is `carry-forward` (issues move to no cycle, or an operator-chosen next cycle) or `remain` (issues keep the ended cycle, now visibly overdue) — set per project, applied by a scheduled job that only ever reassigns `issue_cycle` rows and appends `issue.cycle_changed` events; it is never permitted to change issue state or delete anything, which is what makes "never silently discarded" true by what the job is allowed to touch rather than by convention.

### Bulk operations: one transaction, one report, per-item event

**Decision.** A bulk endpoint accepts a list of issue IDs and a single change (state, assignee, label add/remove, cycle). Each issue is evaluated for permission independently — the spec requires partial success with an itemised report, so a single forbidden issue in a batch of fifty must not abort the other forty-nine. Implementation: one outer transaction is *not* used for the whole batch (that would make "some succeed" impossible); instead each issue's change is its own small transaction (state update + event append, as in the single-issue path), and the endpoint collects `{issueID, ok, error}` per item and returns the full list. "Each affected issue records its own change in history" falls out of reusing the single-issue transition path per item rather than writing one batched event that would need unpacking later.

### Client: the existing vendored `DataTable` for list view, board columns as a lighter-weight component

**Decision.** The list view is the already-vendored `packages/ui/app/components/ui/data-table/DataTable.vue` (TanStack Table v9, per the foundation design's decision to vendor rather than take Nuxt UI's pinned-to-v8 `UTable`) with issue-specific column definitions — sort, filter, and pagination come from the component the platform already owns, not a second table implementation for projects.

The board view is deliberately **not** built on `DataTable` — a Kanban board is not a table, and forcing one through TanStack Table's row model would fight the grain of both the library and the payload-per-column design above. It is a small dedicated component: columns from `issue_states`, cards paginated per column, drag-and-drop backed by the same state-transition endpoint a manual "move to state" action would call, so keyboard-only board interaction is not a separate code path from drag-and-drop — it is the same request with a different trigger, which is what makes the accessible fallback free rather than a second implementation to keep in sync.

## Risks / Trade-offs

- **Project-scoped event streams are a new shape for the event log** (today: one stream per room). → Contained by keeping the room abstraction unchanged and adding a second stream *kind* rather than modifying `Event` or the signing/ordering mechanism; `server/internal/events/event.go` needs no change, only a new caller pattern for issues.
- **The origin-reference resolving at read time means an extra permission check per issue view.** → Same cost as any message read already pays; not a new class of check, and issues are viewed far less often than messages are sent.
- **Storing both the event log and mutable columns for issue state is duplication in the literal sense.** → Deliberate and bounded: the column is written only by the handler that appends the event, in the same transaction, so it cannot drift from the log that justifies it — the same relationship the platform already has between the event log and read/unread state.
- **Six to nine phases is a lot of interface for a volunteer-maintained project to keep working.** → Phasing below front-loads issues (usable without a board at all) before boards, and boards before cycles/milestones, so a stall after any phase still leaves something used, not something half-built.
- **Bulk operations doing one transaction per item rather than one big transaction is slower for large batches.** → Correct trade for the stated audience; a batch of fifty issues is not a performance-sensitive path, and partial success is a hard spec requirement that a single transaction cannot satisfy.

## Open Questions

- Whether project visibility should support more than public/private (foundation's rooms have this today only as a binary too) — deferred to match whatever `messaging/channels` settles on if it grows a third visibility tier; not blocking for this change.
- Whether saved views need a "default view" concept per project (opening a project lands on somebody's saved view rather than the raw issue list) — left to implementation feedback once views exist and people are using them.
- Exact `carry_forward_policy` defaults (carry-forward vs. remain) — a project-level setting with a sensible default to be chosen during implementation; the spec only requires that the choice exists and is honoured.
