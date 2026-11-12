## Why

The foundation change named this gap directly: "community knowledge is trapped in chat scrollback." TDC members already decide who is doing what in Telegram threads and WhatsApp groups, and that decision evaporates the moment the conversation scrolls past it. There is no record of what was agreed, who owns it, or whether it is done, and no way to search for it later. A separate tracker (GitHub Issues, Linear, Jira) would solve tracking but reopen the original problem in a new place: work would live somewhere the conversation that produced it does not, and somebody would have to retype the context by hand to get from one to the other.

This change builds issues and boards directly on the platform's own chat and event log, so a message can become an issue without leaving the conversation, and an issue can be discussed without leaving the tracker. That seam — not the board, not the sprint — is the reason this belongs in a chat platform rather than beside one.

The specs already exist and were written during the foundation change precisely so this would not require redesigning the data model, identity scheme, or event log: `openspec/specs/projects/issues/spec.md` (9 requirements) and `openspec/specs/projects/boards/spec.md` (7 requirements). This change implements them.

## What Changes

- **Projects** — a named, keyed container for issues with its own membership and visibility, reusing the room membership and `Visible`/`RequireMembership` shape already built for channels.
- **Issues** — title, description, state, author, stable human-readable identifier, assignees, labels, priority, parent/child and blocking relationships with cycle prevention.
- **Issue history as events** — every issue change (creation, state transition, assignment, label, relationship) is appended to the same signed event log that already records messages and room state, not a second audit mechanism.
- **Discussion on issues** — issue comments reuse the channel message pipeline (formatting, attachments, mentions, notifications) rather than a parallel comment system.
- **Linking conversation to work** — a message becomes an issue in one action, and the issue carries a permission-respecting back-reference to the message and channel it came from.
- **Boards and views** — a column-per-state board driven entirely by the states configured on a project, plus saved, shareable list/board views with filters and sort.
- **Cycles** — optional, time-boxed, with an explicit non-destructive rule for incomplete work at cycle end.
- **Progress reporting** — computed from recorded state transitions on read, never a maintained counter that can drift.
- **Milestones** — a lightweight cross-cycle grouping with an advisory target date.
- **Bulk operations** — act on many issues at once with per-item success/failure reporting.
- **Search** — issues indexed into Postgres full-text search using the same `tsvector`/GIN/`pg_trgm` machinery `messages` already uses, behind the existing `Searcher` interface, not a second search stack.

**Deliberately excluded as scope creep** (the audience is a volunteer community and small Tanzanian software teams, not an enterprise programme office):

- No custom workflow engine, no per-project scripting, no automation rules ("when X move to Y"). States and transitions are configurable; *triggers* are not — that is `integrations/webhooks`/`integrations/apps` territory if it is ever wanted, and is not built here.
- No story points, velocity charts, burndown/burnup statistics, or estimation fields. Progress reporting is done/total and a trend line, nothing an agile-coach certification would recognise.
- No custom issue types or per-project schema (no "epics" vs "bugs" vs "tasks" as distinct object types with different fields). One issue shape, configurable states and labels, parent/child for hierarchy.
- No Gantt charts, dependency-critical-path calculation, or resource-leveling. Blocking relationships are recorded and shown; nothing computes a schedule from them.
- No swimlanes, no custom board layouts beyond state columns, no per-user board customisation beyond the general saved-view mechanism.
- No time tracking, timesheets, or billing integration. That belongs, if anywhere, in `directory/engagements`, not here.
- No roadmap Gantt/timeline visualisation in this change — milestones give the roadmap its data; a dedicated timeline view is left for later if the community actually asks for one once milestones exist and are used.
- No cross-project bulk operations or cross-project boards — each board is one project's board.

## Capabilities

### New Capabilities

- `projects/issues`: issues, states and workflow, assignment, labels and priority, relationships, discussion, linking conversation to work, history, search and filtering — implementing `openspec/specs/projects/issues/spec.md` in full.
- `projects/boards`: projects, boards, views, cycles, progress reporting, milestones, bulk operations — implementing `openspec/specs/projects/boards/spec.md` in full.

### Modified Capabilities

None. Both capabilities were specified but not implemented in the foundation change; this change implements them without altering their specs.

## Impact

- **Data model**: new tables for projects, project membership, issues, issue states, issue relations, issue labels, cycles, milestones, and a search table for issues mirroring `message_search`. No existing table changes shape; issue comments are stored as ordinary room messages in a room implicitly associated with the issue (see `design.md`).
- **Event log**: a new family of event types (`issue.created`, `issue.state_changed`, `issue.assigned`, `issue.labeled`, `issue.related`, and so on) alongside the existing room and message types. No change to the event envelope or signing scheme.
- **Permissions**: new entries in the `authz` catalogue (`project.create`, `project.administer`, `issue.create`, `issue.transition`, `issue.assign`, `issue.delete`, and similar), scoped at project level the way channel permissions are scoped at channel level today.
- **Search**: a second `*_search` table and `Searcher`-conformant query set, reusing the existing interface; no new search engine, no new dependency.
- **Clients**: new web screens (project list, issue detail, board, list view, cycle/milestone views) built on the already-vendored shadcn-vue `DataTable`. No new charting dependency — progress reporting uses the existing TanStack Charts adapter established in the foundation change, behind the same fallback interface.
- **Zero-cost self-hosting**: no new runtime dependency of any kind. Everything here runs on the Postgres the platform already requires.
- **Mobile/metered data**: board and list payloads are paginated and scoped per project; a board never ships an entire project's issue history to draw its columns (see `design.md`).
