Scope note: these tasks implement `projects/issues` and `projects/boards` against the platform delivered in the foundation change. No foundation capability is modified; this change only adds. Tasks reference specs for what to build and `design.md` for how.

**Version rule: this change's milestone tags are reserved in the range `v1.32.0-dev` to `v1.40.0-dev`.** The four changes in flight hold separate ranges so that any of them can be tagged without waiting on the others; `v1.0.0-dev` stays unused, held for the foundation's undemonstrated deployment milestone.

**Documentation rule: documentation is written as the work is done, not after it.** Anything an operator or a contributor would need to know is written in the same commit as the capability it describes. A phase that adds an endpoint, a permission, or a configuration setting leaves the relevant document current before its milestone is claimed.

**Phasing rule: every phase ends in a running, demonstrable system.** Each group below closes with a milestone task that must be satisfiable by using the software, not by a passing test suite alone. No phase may leave the default branch in a state where the server does not start or a previously working capability has regressed.

## 1. Phase 1 — Projects exist and issues can be filed

Goal: a project with configurable states, and an issue that can be created, viewed, and moved between them, end to end.

- [x] 1.1 Add the `projects` and `project_members` migrations following the `rooms`/`room_members` shape, with visibility (`public`/`private`) and non-disclosure on the private path; verify a private project and its issues do not appear to a non-member in any query, matching the room precedent (`projects/boards` — Projects)
- [x] 1.2 Add `ScopeProject` to `authz` and the project-scoped permission catalogue entries (`project.create`, `project.administer`, `issue.create`, `issue.transition`, `issue.assign`, `issue.label`, `issue.relate`, `issue.delete`, `project.configure_states`); verify each is rejected at a scope it is not grantable at, the same behaviour `GrantableAt` already enforces for channel permissions
- [x] 1.3 Implement project creation, rename, and visibility change with a project key used in issue identifiers; verify a project's key appears in every issue identifier created under it and cannot collide with another project's key
- [x] 1.4 Add the `issue_states` migration and state CRUD scoped to a project, with position ordering and an `is_completion` flag; verify moving an issue to a nonexistent state is refused and it is unambiguous which states mean the work is complete
- [x] 1.5 Implement state removal that either refuses or requires in-use issues to be moved first; verify no issue is left referencing a deleted state
- [x] 1.6 Add the `issues` table and issue creation (title, optional description, initial state, author) with a stable, human-readable identifier incorporating the project key; verify the identifier is unchanged after the issue is renamed, moved between states, or reassigned
- [x] 1.7 Extend the event log with the `issue.*` type family and a project-scoped event stream per `design.md`; verify `issue.created` and `issue.state_changed` events are signed, ordered, and appended in the same transaction that updates the issue's mutable columns, so the two can never disagree
- [x] 1.8 Implement state transitions recording the actor and time via the event log; verify the transition is visible on the issue immediately and reconstructible by replaying the project's event stream
- [x] 1.9 Implement issue history as a read of the project event stream filtered to one issue, showing field, previous value, new value, actor, and time; verify no code path can alter or remove a recorded entry
- [x] 1.10 Build the minimal web screens — project list, project settings for states, an issue list, and an issue detail view with a state selector; verify a person can create a project, define its states, file an issue, and move it through them without using the API directly
- [x] **MILESTONE 1** — Demonstrate: create a project, configure three states, file an issue, move it through all three states, and view its complete, correctly ordered history — all in the browser. Tag `v1.32.0-dev`.

## 2. Phase 2 — Issues talk back: discussion and the conversation link

Goal: the capability that is the reason this belongs in a chat platform, working both directions.

- [x] 2.1 Implement the issue discussion room, created atomically with the issue as a room of kind `issue-discussion` sharing the issue's ID; verify posting, editing, and deleting a comment behaves identically to an ordinary channel message, including attachment limits and mention notifications (`projects/issues` — Discussion)
- [x] 2.2 Implement comment notification to assignees and prior participants; verify a mention in an issue comment notifies according to the mentioned user's preferences, same as a channel mention
- [x] 2.3 Implement "create issue from a message," recording `originChannel`/`originEvent` as a reference on the `issue.created` event rather than copying content; verify the issue is created with a working reference and the originating message visibly indicates an issue was created from it
- [x] 2.4 Implement permission-respecting resolution of the origin reference at read time, reusing `Visible`/`RequireMembership`; verify a user without access to the originating channel sees that an issue references a conversation but never its content, and cannot distinguish a private origin from a deleted one by response shape or timing
- [x] 2.5 Build the "create issue from this message" action in the web message context menu, pre-filling title and description from the message; verify the resulting issue carries a working, permission-respecting back-reference and the origin message shows the created-issue indicator
- [x] **MILESTONE 2** — Demonstrate: two people discuss a bug in a channel, one turns the message into an issue, the issue detail view shows the originating message to a channel member and shows only a non-disclosing reference to someone outside the channel, and a comment posted on the issue behaves exactly like a channel message, mentions included. Tag `v1.33.0-dev`.

## 3. Phase 3 — Assignment, labels, priority, and relationships

Goal: an issue carries enough structure to actually be tracked, not just filed.

- [x] 3.1 Implement assignment to one or more users with notification on assignment; verify an assignee is notified per their preferences and unassignment is recorded with actor and time via the event log
- [x] 3.2 Implement a cross-project "my issues" view; verify it lists issues assigned to the viewer across every project they can access and none they cannot
- [x] 3.3 Implement project-configurable labels and a project-configurable priority scale; verify filtering by label returns only matching issues and sorting by priority respects the configured order
- [x] 3.4 Implement issue relationships (parent/child, blocks/blocked-by) with cycle prevention; verify a relationship that would create a cycle is refused with an explanation, and a parent issue's view reflects the completion state of its children
- [x] 3.5 Extend the web issue detail view with assignees, labels, priority, and a relationship editor; verify all four are usable end to end from the browser
- [x] **MILESTONE 3** — Demonstrate: an issue is assigned to two people who are both notified, carries two labels and a priority, has a child issue whose completion is reflected on the parent, and a blocking relationship that a cyclic attempt correctly refuses. Tag `v1.34.0-dev`.

## 4. Phase 4 — Search and filtering

Goal: issues are findable the same way messages already are.

- [x] 4.1 Add the `issue_search` table mirroring `message_search` — generated `tsvector`, GIN index, a separate table from `issues` — with index/unindex wired into issue creation and edits; verify reindexing an issue's title or description does not write to the `issues` table
- [x] 4.2 Implement `SearchIssues` using `websearch_to_tsquery` with the project-membership join as the authorization boundary, following `SearchMessages`'s pattern exactly; verify hostile input never raises a syntax error and a project the caller cannot access never appears in results
- [x] 4.3 Implement combined filtering by text, state, assignee, label, priority, and project through the existing `Searcher` interface; verify several filters applied together return only issues satisfying all of them
- [x] 4.4 Build the web search and filter interface for issues, including a combined issue+conversation search that also queries `SearchMessages` for an issue's discussion room; verify both are usable end to end and respect the same access boundaries as browsing
- [x] **MILESTONE 4** — Demonstrate: search across all accessible projects returns matching issues ranked by recency, combining a text query with a state and label filter narrows correctly, and a private project's issues are absent from every search performed by a non-member. Tag `v1.35.0-dev`.

## 5. Phase 5 — Boards

Goal: the column view, built to the payload discipline the audience's data plans require.

- [x] 5.1 Implement the board query — paginated issue summaries per state column, derived from the project's ordered `issue_states`, never a full-project fetch; verify a project with hundreds of issues returns only the requested page per column and the response size is bounded regardless of project size
- [x] 5.2 Implement moving an issue between board columns as a compare-and-set state transition sharing the Phase 1 transition path; verify moving a card changes the issue's state, is recorded like any other transition, and two simultaneous moves on the same issue converge deterministically rather than one silently overwriting the other
- [x] 5.3 Build the web board component as a dedicated view rather than a repurposed data table, with drag-and-drop backed by the same transition endpoint a manual move would call; verify keyboard-only column-to-column movement works through the identical request path as drag-and-drop
- [x] 5.4 Verify the board's initial-load payload against a documented budget on a project seeded with hundreds of issues, following the foundation change's payload-budget CI precedent; verify exceeding the budget fails the build
- [x] **MILESTONE 5** — Demonstrate: open a project's board on a throttled mobile connection, move a card by drag-and-drop and confirm the state change, move a second card by keyboard alone, and confirm the initial board payload stays within the documented budget regardless of how many issues the project holds. Tag `v1.36.0-dev`.

## 6. Phase 6 — Views and bulk operations

Goal: people can save how they like to look at a project, and act on more than one issue at a time.

- [ ] 6.1 Implement saved views (filter, sort, list-or-board) storing configuration rather than results, re-run under the viewer's own permissions on open; verify a shared view opened by another user shows only issues that user can see, even when it differs from what the view's owner sees
- [ ] 6.2 Build the web interface for saving, naming, and sharing a view; verify a saved view is reusable later and a shared view is reachable by the people it was shared with
- [ ] 6.3 Implement bulk state, assignee, label, and cycle changes across selected issues, evaluating each issue's permission independently and reporting per-item outcome; verify a batch with some issues the caller cannot modify applies the successful ones and reports exactly which failed and why
- [ ] 6.4 Verify each issue in a bulk operation records its own change in its own history; verify a partially-successful batch leaves no affected issue without its own recorded event
- [ ] 6.5 Build the web bulk-selection and bulk-action interface on the list view; verify a multi-issue selection, action, and outcome report are usable end to end
- [ ] **MILESTONE 6** — Demonstrate: save a filtered board view and share it with a second account that sees it correctly narrowed to their own permissions, then select a mixed batch of issues — some the acting user may modify and one they may not — apply a bulk label change, and see an itemised report of what succeeded and what was refused. Tag `v1.37.0-dev`.

## 7. Phase 7 — Cycles and milestones

Goal: optional time-boxing and cross-cycle grouping, without ever discarding work.

- [ ] 7.1 Add `cycles` and `issue_cycle` with a project-level carry-forward policy; verify a project that does not use cycles retains full issue and board functionality unchanged
- [ ] 7.2 Implement cycle-end handling that reassigns or retains incomplete issues per the configured policy, never deletes; verify an ended cycle with incomplete issues either carries them forward or leaves them assigned, and no issue is silently removed regardless of policy
- [ ] 7.3 Add `milestones` and `issue_milestone`, spanning cycles, with an advisory target date; verify a milestone reports its issues and their completion, and a passed target date marks it overdue without altering any issue automatically
- [ ] 7.4 Build the web cycle and milestone screens, including assigning issues to a cycle or milestone from the issue detail and board views; verify both are usable end to end
- [ ] **MILESTONE 7** — Demonstrate: run a two-week cycle containing several issues, let it end with some incomplete, confirm the configured carry-forward or retain behaviour took effect on every one of them, and show a milestone spanning two cycles whose target date has passed displaying as overdue with its issues untouched. Tag `v1.38.0-dev`.

## 8. Phase 8 — Progress reporting

Goal: the numbers a project or cycle shows are trustworthy because they are derived, not maintained.

- [ ] 8.1 Implement progress computation (complete vs. total, and trend over the period) as a read-time aggregate over current issue state, never an independently maintained counter; verify recomputing progress from the event log's recorded transitions matches the reported figures exactly
- [ ] 8.2 Wire progress reporting into the existing chart-adapter interface established in the foundation change, with the documented readable-figures fallback when a chart cannot render; verify the fallback shows the same numbers the chart would have, not a placeholder
- [ ] 8.3 Build the web progress views for a project, a cycle, and a milestone; verify each is usable end to end and updates immediately after a state transition, with no caching lag a user would notice
- [ ] **MILESTONE 8** — Demonstrate: a project and one of its cycles both show correct, matching progress figures, a state transition performed live updates the figures without a page reload delay, and disabling chart rendering (simulated) still leaves the underlying numbers legible. Tag `v1.39.0-dev`.

## 9. Documentation and cross-cutting verification

Goal: the capability is something a person can pick up from the docs alone, and it does not regress what the platform already had.

- [ ] 9.1 Write the user guide section covering project creation, issue lifecycle, boards, cycles, milestones, and the message-to-issue link; verify a person who has not seen the repository can follow it to file, discuss, and track an issue to completion
- [ ] 9.2 Extend the integration guide with the new endpoints (issue and project routes, bulk operations, search) following the existing machine-readable route description; verify a route added without documentation fails the same check the foundation change put in place
- [ ] 9.3 Add end-to-end tests for the conversation-to-issue link — creation from a message, origin-reference non-disclosure to a non-member, and the reverse indicator on the source message; verify they pass on the existing cross-browser CI backbone
- [ ] 9.4 Add end-to-end tests for board concurrency (two simultaneous moves converge) and bulk-operation partial failure reporting; verify both pass in CI, not only locally
- [ ] 9.5 Extend the existing payload-budget and accessibility CI checks to cover the board and issue-detail screens; verify a seeded violation or an oversized board payload fails the build, matching how the foundation change's checks already behave for chat
- [ ] **MILESTONE 9** — Demonstrate: a newcomer follows only the published user guide to create a project, file an issue from a live conversation, move it across a board, and see it reflected correctly in a milestone's progress — and the full existing chat test suite from the foundation change still passes unchanged. Tag `v1.40.0-dev`.
