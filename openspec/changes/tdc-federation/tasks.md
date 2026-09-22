Scope note: this change implements `federation/protocol` in full, against the event log, addressing, and `server.visibility`/`VisibilityAllowedServers` settings the foundation change already built. See `design.md` for how each requirement is met and for the parts — key revocation, a home server disappearing, membership staleness — that remain genuinely hard after this change ships.

**Version rule: this change's milestone tags are reserved in the range `v1.2.0-dev` to `v1.9.0-dev`.** The four changes in flight hold separate ranges so that any of them can be tagged without waiting on the others; `v1.0.0-dev` stays unused, held for the foundation's undemonstrated deployment milestone.

**Documentation rule: documentation is written as the work is done, not after it.** A phase that adds a setting, an endpoint, or a constraint leaves the relevant document (hosting guide, protocol document, integration guide) current before its milestone is claimed.

**Phasing rule: every phase ends in a running, demonstrable system**, shown with two real TDC server instances rather than a mock peer, wherever the requirement is about servers talking to each other. A phase's milestone is the gate.

## 1. Phase 1 — Servers can find and identify each other

Goal: two standalone servers, still not exchanging any content, can discover each other's identity and prove it belongs to the domain it claims.

- [x] 1.1 Add `federation.enabled` (off by default) gating every federation route except the well-known document itself; verify a disabled server still serves its own identity but refuses every other federated route with a clear reason (`federation/protocol`)
- [x] 1.2 Implement `GET /.well-known/tdc-federation` serving canonical address, `Keyring.Published()` (current and retired keys), and supported protocol versions; verify the response matches the running server's actual keyring, including a retired key
- [x] 1.3 Implement a discovery client that fetches and caches a peer's well-known document with a TTL, re-fetching on a verification failure against an unrecognised key; verify a cached document is reused within its TTL and a forced refetch picks up a rotated key
- [x] 1.4 Implement domain verification — a peer's claimed identity is trusted only when fetched from the domain it names; verify a server presenting an identity that does not match its claimed domain is refused before any content is exchanged (`federation/protocol`)
- [x] 1.5 Implement protocol version advertisement and negotiation, refusing cleanly with a logged reason when two servers share no common version; verify two servers configured with disjoint version sets refuse with a stated reason rather than failing obscurely (`federation/protocol`)
- [x] 1.6 Document the discovery document's shape and the domain-verification rule in the protocol documentation (new `docs/federation-protocol.md` or equivalent); verify a reader unfamiliar with the code can describe what a server must publish and why the domain check exists
- [x] **MILESTONE 1** — Demonstrate: run two TDC servers with federation enabled on different addresses, `curl` each one's well-known document, and show the discovery client on one successfully fetching, caching, and verifying the domain of the other's identity — then show it refusing a third instance whose well-known document claims a domain it is not actually served from. Tag `v1.2.0-dev`.

## 2. Phase 2 — Every federated request is authenticated

Goal: servers can prove requests are really from them, really meant for the recipient, and not a replay — with nothing federated yet actually happening.

- [x] 2.1 Implement RFC 9421 request signing covering `@method`, `@path`, `@authority`, `content-digest`, and a `created`/`expires` window, using the sending server's current signing key; verify a signed request's signature verifies against the sender's published key and fails if any covered field is altered in transit (`federation/protocol`)
- [x] 2.2 Implement verification middleware applied to every federated route: fetch the claimed origin's keys via discovery, verify the signature, and refuse before the handler runs on any failure; verify an unsigned request is refused with no state change, and a tampered signed request never reaches handler code (`federation/protocol`)
- [x] 2.3 Implement destination binding via the signed `@authority`; verify a request signed for server A and replayed against server B is refused by B (`federation/protocol`)
- [x] 2.4 Implement the replay window: expire signatures outside their `created`/`expires` bounds, and track seen `(KeyID, signature)` pairs inside the window in a bounded, evicting cache; verify a request replayed after its window is refused, and a request replayed within the window to the same destination is also refused (`federation/protocol`)
- [x] 2.5 Build a minimal authenticated federation "ping" endpoint exercising the full signing and verification path with no room or event content, purely to prove the transport works in isolation before anything real rides on it; verify a signed ping between two servers succeeds and an unsigned or mis-destined one is refused
- [x] **MILESTONE 2** — Demonstrate: two running servers exchange the signed ping successfully; a captured ping replayed after its expiry is refused; a ping signed for one server's address and sent to the other is refused; each refusal is visible in the server log with a stated reason. Tag `v1.3.0-dev`.

## 3. Phase 3 — Operators control who they federate with

Goal: the enablement, trust, and blocking surface an operator actually touches, before any room ever federates.

- [ ] 3.1 Implement `federation.mode` (`open` | `allowlist` | `blocklist`) and `federation.allowed_servers` / `federation.blocked_servers` settings, following the existing `server.visibility` / `VisibilityAllowedServers` pattern; verify each mode's effect on the ping endpoint from Phase 2 against a listed and an unlisted peer (`federation/protocol`, `platform/server-config`)
- [ ] 3.2 Enforce blocking at the request-verification layer, before signature checking, and at the outgoing-delivery layer, before dialing, from the same list; verify a blocked peer's incoming requests are refused and no outgoing request is attempted toward it, and that blocking takes effect immediately on an operator's change with no restart (`federation/protocol`)
- [ ] 3.3 Record an audit entry for every change to federation enablement, mode, and list membership, using the existing audit mechanism; verify each change type produces a distinct, attributable audit entry (`federation/protocol`, `platform/audit-log`)
- [ ] 3.4 Build the operator-facing settings screen for enabling federation and managing the allow/block lists; verify an operator can enable federation, block a peer, and see the effect reflected in the ping endpoint without using the API directly
- [ ] 3.5 Write the operator-facing federation section of the hosting guide — what enabling it means, what a blocklist versus allowlist does, and the operational cost named in `design.md` (a room's remote participants lose it when its home server is down); verify the guide states this trade explicitly rather than presenting federation as free
- [ ] **MILESTONE 3** — Demonstrate: an operator enables federation on two servers, adds each to the other's allowlist, and the Phase 2 ping succeeds between them; the operator then blocks one from the other, and the ping is refused in both directions immediately, with both the enablement and the block visible in the audit log. Tag `v1.4.0-dev`.

## 4. Phase 4 — A room federates read-only

Goal: a room created on one server becomes visible, with its events verifiable, from a peer — no remote posting yet.

- [ ] 4.1 Add the `federated` flag and participating-peer list with a per-peer delivery cursor to the room model; verify a room defaults to not federated and opting in is an explicit moderator/operator action, not implicit (`federation/protocol`)
- [ ] 4.2 Implement an authenticated federated endpoint serving a room's events since a given cursor, scoped to peers actually participating in that room; verify a peer with no participant in the room receives nothing for it, and a participating peer receives exactly the events after its cursor (`federation/protocol`)
- [ ] 4.3 Implement incoming content verification: resolve the claimed origin's keys via discovery, verify each incoming event with the existing `events.Keyring.Verify`, and reject on failure without storing; verify a forged event (bad signature) and an impersonating event (`Sender`'s server not matching `Origin`) are both rejected and never stored (`federation/protocol`, `platform/event-log`)
- [ ] 4.4 Store verified incoming events preserving the home server's assigned sequence, never renumbering locally; verify a receiving server's local order for a federated room's events matches the home server's order exactly (`federation/protocol`)
- [ ] 4.5 Implement idempotent storage of incoming events keyed on the event's own content-derived ID; verify redelivering an already-stored event is a no-op and produces no duplicate (`federation/protocol`, `platform/event-log`)
- [ ] 4.6 Build minimal read-only display of a federated room's events in the web client, sourced from the receiving server's local replica; verify a person on server B can read messages posted on server A in a room federated between them
- [ ] **MILESTONE 4** — Demonstrate: create a room on server A, federate it to server B, post several messages on A, and show them appearing in correct order on B's client — then show a hand-crafted event with a forged signature submitted directly to B's federation endpoint being rejected and never appearing. Tag `v1.5.0-dev`.

## 5. Phase 5 — Remote users join and post

Goal: the write path. A user whose home server is not the room's home server participates fully.

- [ ] 5.1 Implement cross-server room join: a user's home server requests membership from the room's home server, which enforces its own join rules exactly as for a local user; verify a remote user's join is accepted or refused by the room's home server's actual permission rules, not the joining server's (`federation/protocol`)
- [ ] 5.2 Implement remote posting: a member posts through their own home server, which forwards the event to the room's home server for sequencing, which then propagates it to every other participating server; verify a message posted by a remote member appears, correctly attributed to `user@their-home-server`, to participants on every other participating server (`federation/protocol`)
- [ ] 5.3 Enforce room permissions on remote users identically to local ones at the home server; verify a remote user attempting an action their room permissions disallow is refused by the room's home server the same way a local user would be (`federation/protocol`)
- [ ] 5.4 Implement push delivery from the home server to each participating peer with retry and exponential backoff on failure, reusing the reconnection-backoff shape already built for client connections; verify a temporarily unreachable peer's delivery is retried with increasing backoff rather than tight-looping
- [ ] 5.5 Implement pull-based catch-up: a returning peer requests events since its own cursor rather than waiting out the retry queue; verify a peer offline for a period catches up in one request covering the full gap, with no duplicates against anything it already has
- [ ] 5.6 Implement permanent-unreachability handling: past a configured retry ceiling, stop retrying, record the fact, and surface it to the operator; verify a peer that never becomes reachable again is abandoned rather than retried indefinitely, and the operator sees why (`federation/protocol`)
- [ ] 5.7 Verify home-server-outage isolation: a participating (non-home) server becoming unreachable does not affect the room for participants on other servers, but the room's home server becoming unreachable does make it unavailable to remote participants — write a test asserting the first and documenting the second as the accepted trade rather than a defect (`federation/protocol`)
- [ ] 5.8 Build the web client UI for joining a room on a remote server and posting into it; verify a person can join a federated room hosted elsewhere and send a message that remote and local participants both see
- [ ] **MILESTONE 5** — Demonstrate: a user on server B joins a room hosted on server A, posts a message, and a user on server A sees it live; disconnect server B's network entirely, post several messages on A, reconnect B, and show B's client catching up to exactly the missed messages with nothing duplicated. Tag `v1.6.0-dev`.

## 6. Phase 6 — Connected servers are visible and browsable

Goal: finish the `TDC-Peer-Server` stub `httpapi/visibility.go` already left — real peer discovery and room browsing, not a header nobody legitimately sets.

- [ ] 6.1 Populate `TDC-Peer-Server` from the verified origin of a signed federated request rather than trusting a client-supplied header; verify the visibility checks already in `httpapi/visibility.go` now receive a value the requesting server cannot forge (`federation/protocol`)
- [ ] 6.2 Implement `GET /federation/rooms`, an authenticated federated endpoint returning the rooms this server publishes to the requesting peer under `server.visibility` and each room's own visibility; verify a limited server returns nothing to a peer it has not listed, and a public server's private rooms are still excluded (`federation/protocol`)
- [ ] 6.3 Implement a client-facing endpoint listing this server's known peers with their visibility as seen by this server; verify a person can retrieve the list of connected servers this server can currently reach
- [ ] 6.4 Implement browsing a peer's published rooms through this server, calling 6.2 on the person's behalf; verify browsing a public peer lists its public rooms, and a private room on that same public peer is not returned (`federation/protocol`)
- [ ] 6.5 Build the web client screens for viewing connected servers and browsing a peer's rooms; verify a person can see connected servers and browse a public one's rooms end to end without using the API directly
- [ ] **MILESTONE 6** — Demonstrate: with servers A and B connected (from Phase 3) and a room federated between them (from Phase 4), a person on a third server C — connected to A but not federated into any shared room — browses A's connected-servers list, sees B listed, and browses B's public rooms; a private room on B is not shown to C. Tag `v1.7.0-dev`.

## 7. Phase 7 — Local authority and privacy hold across the boundary

Goal: a receiving server's own moderation and privacy rules apply to federated content exactly as the spec requires, independent of the origin.

- [ ] 7.1 Apply the existing redaction mechanism to federated events, local-only and without contacting or requiring cooperation from the origin server; verify a moderator on the receiving server redacts a federated message and it becomes unretrievable for that server's users while the origin server's own copy is untouched (`federation/protocol`, `platform/event-log`)
- [ ] 7.2 Apply existing content filters, reporting, and moderator actions to federated content identically to local content; verify a filtered word in a federated message is handled (flag/mask/block) the same as a local one, and a report against federated content reaches the same moderation queue (`federation/protocol`, `platform/moderation`)
- [ ] 7.3 Scope every federated response (room listing, event delivery) to peers with an actual participant in the room in question, never by content visibility alone; verify a peer with no member in a private room receives no mention of it even under an allowlist that would otherwise permit discovery (`federation/protocol`)
- [ ] 7.4 Verify no federation endpoint added in this change discloses local user directory data, presence, or membership of rooms the requesting peer does not participate in; write a test asserting the federated room-listing and event-delivery responses contain no fields outside the room the peer participates in (`federation/protocol`)
- [ ] 7.5 Update the moderation section of the user and hosting guides to state that local moderation now applies to federated content, and that redaction is unilateral and does not notify or require the origin server; verify the documentation states this plainly
- [ ] **MILESTONE 7** — Demonstrate: with a room federated between A and B, a moderator on B redacts a message that originated on A; the message is gone from B's client immediately, remains fully intact on A's client, and B's audit log records the redaction with no request having been sent to A. Tag `v1.8.0-dev`.

## 8. Phase 8 — The protocol is documented, versioned, and the hard parts are named where an operator will read them

Goal: everything needed for someone to implement a compatible server independently, and for an operator to understand what federation does and does not promise.

- [ ] 8.1 Write the complete federation protocol document — discovery, signing, event format, delivery, and version negotiation — sufficient for an independent implementation without reading the reference source, per the spec's own requirement; verify a developer unfamiliar with the codebase can describe the full request/response cycle for room federation from the document alone
- [ ] 8.2 Version the protocol document itself and confirm the version-negotiation path from Phase 1 (1.5) is exercised by an intentionally mismatched pair of test server builds; verify two servers configured to support disjoint version sets refuse to federate with the documented reason, and the protocol document states what a version bump requires
- [ ] 8.3 Document key rotation and its actual limits — that a compromised key remains valid to a peer until that peer's cached discovery document expires, and that there is no revocation mechanism beyond rotation — in both the protocol document and the hosting guide; verify the documentation states the exposure window rather than implying revocation is instant
- [ ] 8.4 Document the home-server-loss and peer-loss distinction from `design.md` — a participating server's loss affects only its own users, a home server's loss makes the room unavailable to remote participants — in the hosting guide, in terms an operator evaluating whether to host rooms for other communities can actually use
- [ ] 8.5 Write the federation section of the integration guide, covering how a program interacts with a federated room through the existing API-token mechanism (`platform/api`), since nothing about federation changes how a bot or integration authenticates locally; verify the documented example works against a live federated pair
- [ ] 8.6 Add end-to-end tests covering the full federation cycle — discovery, signed request, room federation, remote join, remote post, redaction, blocking — run against two real server instances in CI; verify the suite catches a deliberately broken signature check and a deliberately broken destination-binding check
- [ ] **MILESTONE 8** — Demonstrate: following only the published protocol document and the hosting guide, deploy two independent TDC servers, federate a room between them, exchange messages from both sides, block one from the other, and confirm the block takes effect — with every step traceable to a specific section of the documentation rather than to source code. Tag `v1.9.0-dev`.
