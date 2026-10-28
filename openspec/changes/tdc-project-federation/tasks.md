Scope note: these tasks extend `projects/boards`, `projects/issues` and `federation/protocol` so a project, its issues and their discussions can be shared between servers. Tasks reference specs for what to build and `design.md` for how.

**Version rule: this change's milestone tags are reserved in the range `v1.41.0-dev` to `v1.48.0-dev`.**

**Documentation rule: documentation is written as the work is done, not after it.** The federation protocol document, the hosting guide and the projects guide stay current with each phase.

**Phasing rule: every phase ends in a running, demonstrable system.** Each group closes with a milestone satisfiable by using the software, not by a passing test suite alone, and no phase regresses an unshared project.

## 1. Phase 1 — A project's stream is complete

Goal: everything a project records is an event, so the stream alone describes the project. Nothing is shared yet.

- [x] 1.1 Audit every write path in `server/internal/projects` and list those that change rows without appending an event (expected: state, label and priority configuration; cycles and milestones; membership; rename and visibility); verify the list against the code by a test that fails when a write path appends nothing
- [x] 1.2 Add an event type for each, carrying everything needed to reproduce the change, appended in the same transaction as the row change; verify each is signed, ordered and shown in the project's history
- [x] 1.3 Implement the projector: a pure function from (rows, next event) to rows for every project event type, unused on the home server for now; verify replaying a project's stream into an empty database reproduces the home server's rows exactly, for a project exercising every event type
- [x] 1.4 Add the replay test to CI, so a new event type without a projection fails the build
- [x] **MILESTONE 1** — Demonstrate: a project with states, labels, priorities, cycles, milestones, members and issues in every state is replayed from its stream into an empty database and the board, lists and progress figures read identically. Tag `v1.41.0-dev`.

## 2. Phase 2 — Remote members

Goal: the home server can hold people from other servers in a project, before anything is replicated.

- [x] 2.1 Extend `project_members` to hold remote members by address, with a role; verify a remote member's role is recorded, listed and changeable only on the home server
- [x] 2.2 Resolve permissions for remote members from their project role alone; verify a remote member without `issue.transition` is refused exactly as a local member would be, naming the permission
- [x] 2.3 Add the project-level federation switch (opened / not opened), administered by `project.administer`, audited; verify a project not opened refuses every federated request about it as missing
- [x] **MILESTONE 2** — Demonstrate: a project on server A lists a member from server B with a role, the switch is shown and audited, and nothing has left server A. Tag `v1.42.0-dev`.

## 3. Phase 3 — Replicas

Goal: a project and its issues' discussions arrive on the servers entitled to them, and nowhere else.

- [ ] 3.1 Replicate an opened project's room to the servers entitled to it (any trusted peer holding a follower for a public project; servers hosting a member for a private one) through the existing room delivery, cursors and catch-up; verify a private project reaches a server hosting a member and no other server, with a test whose project has several peers so a vacuous pass is impossible
- [ ] 3.2 Run the projector on replicas, writing replica rows only from events, in order; refuse an unknown event type by stopping that project's stream with a visible reason rather than skipping; verify a replica that followed live and one rebuilt from the start hold identical rows
- [ ] 3.3 Replicate each issue's discussion room with its project, to the same servers; verify a comment on the home server appears on the replica in order, and a new issue's discussion is federated from its first message
- [ ] 3.4 Stop and discard a replica when its server loses entitlement (last member removed, project made private or closed to federation); verify the server stops receiving events and its local rows are removed
- [ ] 3.5 Show a shared project on a replica: read-only marking, the home server's name, and its state when the home is unreachable; verify a replica's board, lists, progress and saved views work for local readers
- [ ] **MILESTONE 3** — Demonstrate: a private project on A with a member on B appears on B with its board and an issue's discussion, does not appear on C, and disappears from B when that member is removed. Tag `v1.43.0-dev`.

## 4. Phase 4 — Changes from anywhere

Goal: members on replicas work in the project as if it were local.

- [ ] 4.1 Send changes made on a replica to the home server as signed federated requests in the author's name, for every issue change (file, edit, move, assign, label, prioritise, relate, plan); verify the home server authorises each against its own records and the change returns to the replica as an event, and that a replica never allocates a sequence in a project stream
- [ ] 4.2 Refuse a request whose author does not belong to the requesting server, and one for a project the requester holds no replica of; verify both are refused and recorded
- [ ] 4.3 Comments from replicas through the existing remote-message path; verify mention and assignment notifications reach a person through their own server
- [ ] 4.4 Pending state in the interface between making a change and its event arriving, and a clear refusal when the home server says no; verify a refused move puts the card back with the reason
- [ ] 4.5 Public projects: following from a peer (read-only), and requesting membership; verify a follower reads and cannot change anything
- [ ] **MILESTONE 4** — Demonstrate: a member on B files an issue in A's project, a member on A moves it, a member on B comments and is assigned, and the board on both servers shows the same issue in the same column with the same history. Tag `v1.44.0-dev`.

## 5. Phase 5 — Ready to run

- [ ] 5.1 Federation protocol document: the project event family, the projector rules, the change-request endpoints, entitlement and discard; verify an independent implementer could build a replica from the document alone
- [ ] 5.2 Hosting guide and projects guide sections for sharing projects; verify an operator can open a project to federation from the guide alone
- [ ] 5.3 End-to-end checks across two servers in the browser suite (Chromium and Firefox locally, WebKit in CI)
- [ ] 5.4 An operator command that rebuilds a replica from its stream
- [ ] **MILESTONE 5** — Demonstrate the full two-server journey from a stranger's reading of the guides. Tag `v1.45.0-dev`.
