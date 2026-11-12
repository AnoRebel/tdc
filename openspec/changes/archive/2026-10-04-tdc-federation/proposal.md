## Why

TDC servers are standalone today. A chapter, a company, or an individual can run one, but their members can only talk to people on that same server — which reproduces the very lock-in TDC left Telegram and WhatsApp to escape, just with TDC's own name on it. The foundation change built the event log, the `user@server` / `room@server` addressing, and `server.visibility` specifically so this would not require a rewrite: `platform/event-log` already states that an event created on one server must be verifiable by another using only the originating server's published keys and the event's own content, and the federation-readiness test (foundation task 10.7) already proves that for a single event in isolation. What does not exist yet is any of the machinery around it — no server has ever published its keys to another, signed a request to another, or accepted an event from one. `httpapi/visibility.go` reads a `TDC-Peer-Server` header today and says so plainly in its own comment: "there is no federation yet... nothing legitimately populates it until peers exist."

This change makes that machinery real: servers discover each other, authenticate every request they exchange, and share rooms whose home server remains authoritative for membership, ordering, and moderation — so that no single organisation, including whoever runs the reference server, is a chokepoint for the community.

## What Changes

This change implements `federation/protocol` in full: server identity and discovery, request signing, home-server-authoritative rooms, cross-server membership and message propagation, content verification, trust and blocklists, connected-server browsing, local policy precedence, and privacy boundaries between servers. It is entirely additive to a running TDC server — a server with federation disabled continues to work exactly as the foundation change left it, which the spec requires explicitly ("Federation is optional").

**Delivered in this change:**

- **Identity and discovery** — a well-known endpoint publishing a server's canonical address, its Ed25519 keys (the `Keyring` type already supports exactly this), and the protocol versions it supports; peer lookup with caching and domain verification.
- **Request authentication** — RFC 9421 HTTP Message Signatures (the scheme already named in the foundation's design) on every federated request, covering method, path, body, destination and timestamp, with a bounded replay window.
- **Federation enablement, trust and blocking** — the operator-facing off switch, an allowlist/blocklist enforced both directions, and audit entries for every trust change.
- **Home-server-authoritative rooms** — a room federates only through its home server; remote servers hold a read replica of that room's event stream and defer to the home server's ordering and membership decisions rather than resolving conflicts themselves.
- **Cross-server membership and message propagation** — a user joining a room on a remote server, posting through their own home server, delivery to every participating server with retry and backoff, gap catch-up on reconnection, idempotent redelivery, and abandonment of a peer that stays unreachable past a configured limit.
- **Content verification** — every incoming event verified against the claimed origin's published keys before it is stored or shown, with impersonation and forged events rejected rather than merely flagged.
- **Connected-server browsing** — finishing what `httpapi/visibility.go` stubbed: a person can see which servers their server is connected to and browse a public or listed peer's published rooms, with room visibility still enforced after server discovery.
- **Local policy precedence and privacy boundaries** — a receiving server can remove federated content for its own users without the origin's cooperation, and a server discloses to a peer only what that peer's participants are entitled to see.
- **Protocol documentation** — the wire protocol published as versioned documentation sufficient for an independent implementation, per the spec's own requirement, alongside the source under the same licence.

**Explicitly out of scope, and why:**

- **Cross-server presence and cross-server search** stay local. The foundation design's open questions left both undecided; this change decides them by omission because both are expensive relative to their value and the spec does not require either. A future change can add them without touching anything built here.
- **A UI for founding a *new* federated room from scratch across two servers simultaneously** is not built; a room federates by an operator or moderator explicitly opening it to peers after creation, which is a smaller and safer surface than negotiating room creation across two servers at once.
- **Directory data sharing** (`directory/profiles`, `directory/engagements`) is unaffected. The spec requires that federating two servers must not implicitly share their user directories, and this change does not build any mechanism that would.

## Capabilities

### New Capabilities

None — `federation/protocol` already exists as a spec from the foundation change. This change implements it; no new capability is declared.

### Modified Capabilities

- `federation/protocol`: moves from specified-but-unimplemented to implemented in full — identity and discovery, request signing, room ownership and cross-server membership, message propagation, content verification, trust and blocking, connected-server visibility and browsing, local policy precedence, and privacy boundaries. `platform/server-config` gains the federation-enablement setting alongside the `server.visibility` and `VisibilityAllowedServers` settings it already has. `platform/audit-log` gains entries for trust and visibility changes, which the spec already requires but which had no federated counterpart to audit until now.

## Impact

- **Runtime dependencies**: none added. Federation is HTTP calls between TDC servers, signed with keys the event log already generates; no message broker, no third-party discovery service, nothing that could violate zero-cost self-hosting. A server with federation disabled needs nothing beyond what it needs today.
- **Data model**: new tables for known peer servers (their published keys, trust state, reachability), for which rooms are federated and to which peers, and for delivery cursors per peer per room — the state needed to retry, catch up, and detect duplicates. No existing table's meaning changes; `events`, `rooms`, and `memberships` gain federation-relevant rows, not federation-relevant columns that would change local-only behaviour.
- **Operators**: a new configuration surface — enable federation, list or block peers, set this server's default federation posture for new rooms — and a new operational reality: a room with remote participants is unavailable to them when this server is down, and a room hosted elsewhere is unavailable to local participants when that server is down. This trade was named and accepted in the foundation's design; this change is where an operator first feels it.
- **Security surface**: this is the first capability that accepts unauthenticated network input from servers this operator does not control. Every requirement in the spec about signature verification, replay refusal, and destination binding exists because of that fact, and the tasks below treat it accordingly — verification is never optional, and a signature failure never partially applies a change.
- **What this change deliberately does not settle**: key distribution has no revocation mechanism beyond a key's own rotation record — a compromised key is handled by rotating it, publishing the rotation, and accepting that anything signed with the old key before the rotation was published remains valid, because nothing in the spec provides a way to backdate trust. A home server that disappears permanently leaves its rooms unrecoverable by design — that is the cost `design.md` already named as the price of avoiding distributed state resolution, and this change does not soften it. Two servers can still disagree about a room's state during the window before a home server's ordering reaches them both; the spec resolves this by declaring the home server's order authoritative and forbidding independent resolution, not by making the window zero.
