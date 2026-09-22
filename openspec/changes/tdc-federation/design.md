## Context

See `proposal.md` for motivation. This document records the technical decisions for implementing `federation/protocol`, and is honest about what remains hard even after they are made.

The foundation change's `design.md` already decided the shape: home-server-authoritative rooms over a TDC-defined protocol, RFC 9421 HTTP Message Signatures, Ed25519, a well-known discovery path, and a narrow internal transport interface so the wire format is a week's change rather than a rewrite. Nothing here reopens those decisions. This document is about what the foundation deliberately left as open questions or simply had no occasion to build: the actual discovery endpoint, the actual signing middleware, the actual retry and catch-up logic, and the operational edges — a key rotating, a peer disappearing, two servers disagreeing.

**What the signed event log already bought.** Every event carries a content-derived identifier, a signature, and a `KeyID`; `events.Keyring.Verify` checks both against a set of published keys with no database access to the signer; `rooms.Verifiable` reconstructs the exact bytes that were signed from a stored row with no joins; and the federation-readiness test (foundation task 10.7) already proves a single event, handed to a fresh process with only the originating server's published keys, verifies. **What it did not buy**: a way to fetch those published keys over the network, a way to know which events belong to which peer, a way to authenticate the request carrying the event (as opposed to the event itself — a stolen session could still submit a correctly-signed event to the wrong place without request signing), retry and catch-up machinery, or membership across servers. The event model is the foundation of the house; this change builds the house.

## Goals / Non-Goals

**Goals:**

- Implement every requirement in `openspec/specs/federation/protocol/spec.md` — no partial requirement, though some (blocking, visibility) build on stubs the foundation already left.
- Every phase ends with two real running TDC server instances demonstrating the capability against each other, not a mock peer.
- Zero new runtime dependencies. Federation is HTTP plus the keys the event log already produces.
- Name the hard parts — key rotation and distribution, permanent peer loss, abuse, eventual consistency, disagreement — in the spec's own terms rather than assuming them away.

**Non-Goals:**

- Cross-server presence, cross-server search, and cross-server directory sharing. Each is a real feature with a real cost; none is required by the spec, and each is strictly additive later.
- Any resolution mechanism for two servers disagreeing about a room's state beyond "defer to the home server." The spec forbids independent conflict resolution; this change does not invent one anyway for the sake of looking more complete.
- A federation-specific UI for creating a room. A room federates by being opened to peers after it exists.

## Decisions

### Discovery: a well-known path serving the server's own `Keyring.Published()` and canonical address

**Decision.** `GET /.well-known/tdc-federation` returns JSON: the server's `canonical_address` (already an operator-set config value, never inferred from a request header — reused as-is), the current and retired public keys from `Keyring.Published()` (already exists, already includes retired keys so history keeps verifying), and the list of protocol versions this server accepts.

A requesting server caches this with a TTL and re-fetches on cache miss or signature-verification failure against a key it does not recognise — the second case matters because a key rotation must become visible to peers without every peer polling constantly.

**Why a static document rather than a negotiated handshake.** The spec asks for exactly this: "publish, at a well-known location... the information another server needs to reach and verify it." A GET with no state matches how TLS certificate discovery and Matrix's own `.well-known` already work, is trivially cacheable, and needs no session. The cost is that revocation is not instant — covered under Trust and blocking below.

**Domain verification, not just retrieval.** The spec requires refusing a peer "whose published identity does not match the domain it claims." Concretely: a server claiming to be `example.org` is only trusted as `example.org` if `https://example.org/.well-known/tdc-federation` itself serves that identity — the claim is verified by fetching it from the domain it names, the same trust anchor DNS and TLS already provide, not by asking the peer to vouch for itself over an already-open connection.

### Request signing: RFC 9421 applied to a `federation` HTTP client and middleware pair

**Decision.** A `federation.Sign(req, keyring)` function that adds an RFC 9421 `Signature` and `Signature-Input` header covering `@method`, `@path`, `@authority`, `content-digest`, and a `created`/`expires` window; a matching middleware that verifies it against the claimed origin's published keys (fetched via discovery, above) before the request reaches any handler.

**Binding to a destination.** The spec requires that "a request signed for one destination server is presented to a different server" is refused. `@authority` is covered by the signature for exactly this: a captured, validly-signed request cannot be replayed against a server other than the one it named, because changing the authority breaks the signature rather than merely looking suspicious.

**Replay window.** `created`/`expires` bound the signature's validity. The receiving server also keeps a short-lived cache of `(KeyID, signature)` pairs seen inside that window, since a timestamp bound alone still allows replay to the *same* destination within the window — the window shrinks the attack, the cache closes it. The cache is bounded and evicts on expiry, matching the design's existing rule that in-process caches must never grow unbounded.

**Why this is a separate signature from the event's own.** An event is signed by its author's home server once, at creation, and that signature travels with the event forever — it says "this content is authentic." A request signature is per-delivery and says "this HTTP call really came from the server it claims, right now, to this destination." Conflating them would mean a stolen or misrouted request could carry a perfectly valid event signature while lying about who is sending it or where it was meant to go. Both checks run; neither substitutes for the other.

### Federation enablement and trust: three settings, following the `server.visibility` pattern exactly

**Decision.** `federation.enabled` (bool, off by default, following the spec's explicit "disabled until the operator enables it") gates every federated endpoint at the router level — disabled means the well-known path still resolves (a server must be able to say "federation is off" rather than connection-refuse, which is what makes "refuses with a clear, logged reason" possible instead of an opaque timeout) but every content-bearing federated route refuses before touching a handler.

Trust is `federation.mode` (`open` | `allowlist` | `blocklist`, default `open` once enabled — mirroring `server.visibility`'s default-public stance, since a federation feature nobody configured should behave predictably rather than silently restrict) plus two settings-backed lists reusing the exact mechanism `VisibilityAllowedServers` already established: `federation.allowed_servers` and `federation.blocked_servers`.

**Blocking is enforced at the same layer as request verification**, before a signature is even checked, so a blocked peer's requests are refused cheaply rather than after doing the cryptographic work — and outgoing delivery checks the same list before dialing, so "blocking... MUST stop both incoming and outgoing exchange" is one list consulted from two directions, not two lists that can drift apart.

**Every change to these three settings is an audit entry**, using the audit mechanism `platform/audit-log` already provides — this is a direct restatement of the spec's own requirement and costs nothing new to build.

### Home-server-authoritative rooms: a room's federation state is a property of the room, not of individual events

**Decision.** A room gains a `federated` flag and, when set, a list of participating peer servers with a per-peer delivery cursor (last sequence number successfully delivered). The room's home server is derived the same way addresses already are — from the server named in `room@server` — so "which server is authoritative" is never a separate field that could disagree with the address itself.

A non-home participating server stores the room's events as they arrive but **never assigns sequence numbers to them** — sequence assignment is the home server's alone, exactly as it already is for local events (`Service.Address`, per-room total ordering from the foundation change). A remote server importing an event keeps the sequence the home server assigned and orders locally by it; it does not renumber, because renumbering is the first step toward the independent conflict resolution the spec forbids.

**What this means concretely for "no independent conflict resolution":** if a remote server receives an event that implies a membership state it does not currently hold — someone posting who it did not think was a member — it does not decide who is right. It defers, meaning it either fetches current membership from the home server before rendering the event to users, or (this change's actual choice) trusts the home server's own claim embedded in the event delivery and reconciles on the next full sync, logging the discrepancy rather than resolving it. **This is the honest limit of the design**, named rather than hidden: between the moment a remote server's cached membership goes stale and the moment it next reconciles, it can show an event from someone whose membership it has not itself re-verified against the home server, because re-verifying on every single event would mean every remote read costs a synchronous round trip to the home server — which defeats the purpose of replicating the room at all. The spec's own accepted trade ("home server outage affects only its own users" cutting the other way: a *live* remote room does not need to ask the home server permission for every read) is what licenses this.

### Cross-server membership and message propagation: push with cursor-based catch-up, not a queue

**Decision.** The home server pushes new events to each participating peer as they are accepted, over the same signed-request mechanism, batched (the design already named "batched events over HTTPS with a cursor for catch-up — the same shape Mattermost ships commercially"). Each peer's delivery cursor is the last sequence number it has acknowledged. On failure, the home server retries with exponential backoff (mirroring the reconnection backoff already built for client connections in `messaging/realtime`, so the shape of "many things retrying should not retry in lockstep" is reused rather than reinvented) up to a configured ceiling, past which the peer is marked unreachable, retries stop, the fact is recorded, and the operator is told — this is the spec's own "permanently unreachable peer is abandoned" scenario, worded almost exactly.

**Catch-up is pull, not just push-retry.** A returning peer calls a `since=<cursor>` endpoint on the home server rather than waiting for the home server's retry queue to walk through backlog — this matters because a peer that was down for a day should not receive a day of individually-retried pushes; it asks once for everything after its cursor. Push and pull share the same underlying "events since sequence N" query, so there is exactly one code path for "what has this peer not seen yet," not two that could disagree.

**Idempotency** is the event's own content-derived ID, already unique by construction (foundation's nonce-based derivation). A peer storing an incoming event first checks whether that ID already exists locally; redelivery is a no-op rather than a duplicate, satisfying "repeated delivery MUST NOT produce duplicates" with the mechanism the event log already has, not a new one.

### Content verification: every incoming event is verified before it is stored, using the discovery-fetched keyring of its claimed origin

**Decision.** An incoming event's `Origin` field names the server whose signature must verify (already part of the `Event` struct and its canonical form). The receiving server resolves that origin's published keys via the discovery mechanism above (cached), builds a verify-only `Keyring` from them (`KeyringFromPublished` already exists and does exactly this), and calls `Keyring.Verify` — the same function that already verifies local events, run against a different keyring. **This is the payoff of the foundation's federation-readiness test**: the verification code was never local-only to begin with, so "verify a foreign event" is "call the existing function with a different key source," not new verification logic that could disagree with the local one.

An event whose `Sender` claims a different home server than the one that signed it (`Origin`) is rejected as impersonation — the spec's own scenario, and a direct field comparison once `Origin` and `Sender`'s server suffix are both in hand.

### Connected servers and browsing: finishing the `TDC-Peer-Server` stub

**Decision.** `httpapi/visibility.go` already implements the visibility *decision* (`mayDiscover`, `listsPeer`) — what it lacks is anything that legitimately sets `TDC-Peer-Server`, and a room-listing endpoint to guard. This change adds: an authenticated federated `GET /federation/rooms` a peer calls (with `TDC-Peer-Server` now populated from the signed request's verified origin, not a client-supplied header — the header becomes a value the server sets after verification, not one it trusts on arrival), returning the rooms this server publishes to that peer under its visibility rules; and a client-facing endpoint listing this server's known peers and letting a person browse a peer's published rooms through it. Room-level visibility (public/limited/private, already implemented for local rooms) is re-checked after server-level visibility passes, exactly as the spec requires ("a private room on a public server remains undiscoverable") — two checks in series, neither one substituting for the other.

### Privacy boundaries: scope every federated response to the requesting peer's actual participation

**Decision.** The event-propagation and room-listing paths both filter by "does the requesting peer have a participant in this room" before including anything, rather than filtering by "is this content public" — a private room the peer has no member in is never mentioned to it at all, regardless of any visibility setting, because visibility governs discovery of rooms a server *offers*, not disclosure of rooms it does not. Directory data (`directory/profiles`) is not exposed by any federation endpoint this change adds; sharing it is explicitly future work requiring its own opt-in, per the spec's "neither gains access to the other's user directory unless explicitly configured to share it."

### Local policy precedence: redaction is a local, unilateral act that does not propagate as a request to the origin

**Decision.** A server's existing redaction mechanism (`platform/event-log`'s redaction, already implemented) is reused, applied to a federated event exactly as to a local one: the content is removed locally, the origin is never asked and cannot refuse, and the redaction does not travel back to the origin server or to other peers — it is this server's decision about what this server shows this server's users. This is a direct implementation of "MUST be able to remove content locally without the cooperation of the originating server," and reusing the existing redaction code means a federated removal and a local removal are audited, retained, and rendered identically; there is no second removal mechanism to keep in sync with the first.

## Hard parts, named honestly

The proposal's Impact section already states the top-level trade-offs; this section is where the mechanism for each is spelled out rather than gestured at.

**Key distribution and rotation.** Distribution is TOFU-with-verification: the first time a server is contacted, its keys are whatever its own `.well-known` currently serves, checked only against the domain the peer claims (DNS plus TLS is the only anchor — there is no third-party certificate authority for TDC server identity, deliberately, because introducing one would be a paid or centralised dependency the zero-cost constraint rules out). Rotation is handled — `Keyring.Rotate` already retires rather than deletes a key, and `Published()` already includes retired keys — but **revocation of a compromised key that is still inside its stated validity has no mechanism**. A server that publishes a rotation is trusted from that point forward; anything signed with the old key before the rotation was published and fetched by a given peer remains valid to that peer until it happens to refresh its cache. This is a real gap, not an oversight: closing it fully needs either a short-TTL discovery document (which trades cache efficiency for exposure window, and is a configuration value future work can tune) or a revocation list (which is new state, new endpoints, and a new failure mode — a server that cannot be reached to check revocation either fails open, defeating the point, or fails closed, meaning peer *availability* now gates every verification). This change picks the short-TTL lever, documents the exposure window, and leaves a revocation list as explicitly future work.

**A server that goes away.** Two distinct cases, both named in the spec and both handled differently: a *participating* server going away (spec: "home server outage affects only its own users" — handled, since other peers keep serving their own users from their own replica) versus the *home* server going away (spec accepts this as the room becoming unavailable to remote participants — not handled, by design, because handling it means exactly the distributed state resolution the whole protocol exists to avoid). An operator reading only the proposal could mistake these for the same failure; they are not, and the tasks below build a test for each so the distinction is enforced rather than merely written down.

**Abuse and blocklists.** The blocklist stops future exchange immediately, per spec. It does not retroactively un-deliver events a now-blocked peer already sent while trusted — those are subject to ordinary local moderation (`platform/moderation`, already built) exactly like locally-posted abusive content, because federation does not get a separate moderation pipeline; abuse from a peer is abuse, handled the way abuse already is. What federation adds is the ability to stop it at the source (block the peer) in addition to removing individual content, which local-only moderation never had to distinguish.

**Eventual consistency of membership.** Named above under home-server-authoritative rooms: a remote server's view of who is a member can be briefly stale between reconciliations. The spec's requirement is that the home server's decision is what counts and other servers "reflect" it, not that they always reflect it with zero latency — "reflect" is satisfied by eventual reconciliation, and this change treats a bounded, logged staleness window as meeting that requirement rather than pretending the window is zero.

**Two servers disagreeing about a room's state.** By construction this cannot become a protocol-level fight: a non-home server has no mechanism to assert room state at all, only to receive and store what the home server sends. Disagreement manifests as one server's replica lagging or diverging from what the home server would currently say if asked directly — which is staleness, covered above, not a conflict requiring resolution. The spec's requirement ("it defers to the home server rather than computing its own resolution") is satisfied structurally: there is no code path in this design where a non-home server computes room state independently, so there is nothing to disagree with.

## Risks / Trade-offs

- **The discovery cache means a rotated or revoked key is not instantly untrusted everywhere.** → Documented above; short TTL is the lever operators have, and the exposure window is stated rather than hidden.
- **Cross-server delivery adds a genuinely new operational surface — retry queues, dead-letter state, per-peer cursors — that a single-server operator never had to think about.** → Federation is opt-in and off by default; a server that never enables it carries none of this state or code path in its running behaviour beyond serving its own well-known document.
- **The home-server-authoritative model means TDC cannot promise "your messages are always available" the way a fully-replicated system could.** → This is the foundation design's accepted trade, restated here because federation is where an operator first experiences it rather than reads about it.
- **RFC 9421 request signing is new middleware on every federated endpoint, and a bug there fails either open (a security hole) or closed (federation silently stops working).** → Verification failure is fail-closed by construction, matching the spec's "unsigned request is refused... no state changes"; the tasks below require a test proving a tampered request is rejected before any handler-level state change occurs, not merely before a response is sent.
- **Reusing local redaction for federated content means a very active moderator on a small server could, in principle, redact a lot of federated content without the origin ever knowing.** → This is the spec's own stated design ("MUST be able to remove content locally without the cooperation of the originating server"), not an accident; audit logging of every redaction is what gives an operator visibility into their own moderator's actions, which is the correct place for that control to live.

## Open Questions

These can be answered during implementation without changing the spec or the task breakdown:

- The exact backoff schedule and unreachable-peer ceiling — the spec requires retry-with-backoff and eventual abandonment; the specific numbers are an operational tuning question, not a design one, matching how the foundation left memory and payload budgets as figures to set once there is something to measure.
- Whether the discovery document's cache TTL should be operator-configurable per deployment or fixed — fixed is simpler to reason about; configurable serves an operator who wants a tighter revocation window at the cost of more discovery traffic.
- The wire encoding for batched event delivery (compact binary versus signed JSON) — the foundation's design already declared this out of scope for the spec and a later implementation detail; this change picks one during implementation without it being a design decision worth blocking on.
