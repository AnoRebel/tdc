Scope note: this implements `integrations/webhooks`, `integrations/apps`, and the remainder of `integrations/interactive-messages`. API tokens (`server/internal/apitokens/`), the message-action schema and validation (`server/internal/actions/`), exactly-once button presses (`server/internal/rooms/presses.go`), the generated API description, and the worked integration example in `docs/integrating.md` already exist from the foundation change and are extended here, not rebuilt. Tasks reference specs for what to build and `design.md` for how.

**Version rule: this change's milestone tags are reserved in the range `v1.12.0-dev` to `v1.17.0-dev`.** The four changes in flight hold separate ranges so that any of them can be tagged without waiting on the others; `v1.0.0-dev` stays unused, held for the foundation's undemonstrated deployment milestone.

**Documentation rule: documentation is written as the work is done, not after it.** Anything an operator or an integrator would need to know is written in the same commit as the capability it describes. `docs/integrating.md` already exists; this work extends it phase by phase rather than adding a new document at the end.

**Phasing rule: every phase ends in a running, demonstrable system.** Each phase closes with a milestone satisfiable by running the software, not by a passing test suite alone. No phase may leave the server unable to start or a previous milestone regressed.

## 1. Phase 1 — A message can carry more than buttons

Goal: close the built part of `integrations/interactive-messages` against the spec — selection menus, input fields, and the three response modes — before anything talks to an external process at all.

- [x] 1.1 Add selection-menu and input-field element kinds to the existing action schema in `server/internal/actions/`, validated by the existing `Check` at post time; verify a message with a menu carrying too many options, or an input with no field name, is refused before posting rather than on render (`integrations/interactive-messages`)
- [x] 1.2 Render menus and inputs in both clients as accessible controls, reusing the button rendering path's keyboard/announcement pattern from the foundation change; verify with a screen reader that a menu is announced as a list of choices and an input as a labelled field
- [x] 1.3 Add a version/generation counter to a message's action state and implement updating the originating message atomically on it; verify two concurrent responses to the same invocation result in one applied update and one refused as stale
- [x] 1.4 Implement private responses delivered only to the invoking user's live connection(s), not through the room's event log; verify a second room member's live connection never receives a private response addressed to someone else
- [x] 1.5 Implement the multi-step context store keyed to (integration, conversation, invoking user), discarded when either is removed; verify one user's stored context is unreadable through any endpoint by another user, and that removing the integration discards it
- [x] 1.6 Extend `docs/integrating.md`'s interactive-messages section with menus, inputs, updating in place, and private responses, alongside the existing button example; verify the documented examples run against a live server
- [x] **MILESTONE 1** — Demonstrate: a message offers a selection menu and an input field in the web client, an update-in-place response replaces its controls for every viewer, a private response is visible only to the person who triggered it, and a stale second activation is refused. Tag `v1.12.0-dev`.

## 2. Phase 2 — Deliveries can be signed and verified

Goal: the signing mechanism every later phase depends on, built once and shared, before there is anything to sign.

- [x] 2.1 Expose the server's Ed25519 public signing keys at a well-known unauthenticated path, reusing `events.Keyring.Published()`; verify a request with no credential receives the current and any retired keys
- [x] 2.2 Implement a delivery envelope — payload, timestamp, and Ed25519 signature over both, following the RFC 9421 shape the foundation design already committed to — as a shared function usable by both webhook delivery and invocation delivery; verify an altered payload byte fails verification and an unaltered one succeeds
- [x] 2.3 Implement the replay window check — a documented number of minutes between the signed timestamp and receipt — as a function the delivery envelope's consumer runs, and document it for integrators; verify a delivery replayed outside the window is detectably invalid by the documented scheme
- [x] 2.4 Write the delivery-signing section of `docs/integrating.md` with a worked verification example in the same language as the existing worked example; verify the documented verification code correctly accepts a real delivery and rejects a tampered one
- [x] **MILESTONE 2** — Demonstrate: fetch the published keys from a live server, receive a hand-crafted signed test delivery, and verify it with the documented scheme using nothing but the published keys and the delivery's own content. Tag `v1.13.0-dev`.

## 3. Phase 3 — Outgoing webhooks deliver, retry, and stay out of the way

Goal: the hard part. A subscription that survives a receiver being down for a day, a slow receiver that cannot stall anything else, and a persistently failing one that gets disabled rather than retried forever.

- [x] 3.1 Implement the `webhook_subscriptions` table and creation/listing/deletion endpoints, scoped to event types the creating user can see and refused for a channel they cannot read; verify a subscription request for a channel the caller cannot read is refused, and that the subscription is described in the generated API description
- [x] 3.2 Implement the `webhook_deliveries` table and a `FOR UPDATE SKIP LOCKED` claim loop delivering over HTTP with a bounded per-request timeout and the SSRF-safe dialer from `server/internal/unfurl/` reused rather than reimplemented; verify a subscription naming a loopback or private address is refused at creation, and that a redirect to one during delivery is not followed
- [x] 3.3 Implement exponential backoff with jitter up to a documented attempt limit, recording every attempt's outcome and response code; verify a receiver that fails twice then succeeds is retried with increasing delay and the third attempt's success is recorded alongside the two failures
- [x] 3.4 Implement disabling a subscription that crosses the documented consecutive-failure threshold, notifying its owner through the existing notification pipeline; verify a subscription whose receiver never responds is disabled rather than retried indefinitely, and its owner receives a notification
- [x] 3.5 Implement per-subscription and server-wide outbound concurrency and rate limits on `cache.Rates`, with exceeded limits recorded and visible to the owner; verify a burst of events against a slow receiver does not open unbounded concurrent connections, and that hitting the limit is visible on the subscription rather than silently dropping events
- [x] 3.6 Bind lost permission to stopped delivery — a subscription tied to a channel the subscribing user can no longer see stops delivering; verify losing channel access silently halts that subscription's future deliveries without deleting its history
- [x] 3.7 Implement the payload schema with an explicit version field, documented and versioned so a breaking change ships as a new version while existing subscriptions keep receiving the old one; verify a subscription created before a hypothetical version bump continues to receive the version it subscribed to
- [x] 3.8 Build the web interface for creating, listing, and deleting outgoing webhook subscriptions and viewing their delivery history — outcome, response code, and timestamp per attempt; verify an operator can see, from the UI alone, which of a subscription's recent deliveries failed and why
- [x] 3.9 Sign every outgoing delivery with the envelope from Phase 2 and document the outgoing-webhook payload shape and signature verification in `docs/integrating.md`; verify a receiver following only the documentation can verify a real delivery from a running server
- [x] **MILESTONE 3** — Demonstrate: subscribe a local HTTP endpoint to message events in a channel, post a message, and observe a signed delivery arrive; stop the receiver, post again, and watch retries appear with increasing delay in the delivery history; leave it down past the failure threshold and observe the subscription disable itself and notify its owner; verify a slow receiver (introduce an artificial delay) does not block chat delivery to the room's live members during the same test. Tag `v1.14.0-dev`.

## 4. Phase 4 — Incoming webhooks post, scoped and revocable

Goal: an external system can post into exactly one channel with a credential that assumes it will leak.

- [ ] 4.1 Implement the `incoming_webhooks` table — room-bound, prefixed and hashed like an API token, created and revoked with immediate uncached effect; verify a revoked webhook's very next call is refused, with no delay
- [ ] 4.2 Implement the posting endpoint accepting the webhook's credential and a message body, attributed to the webhook rather than a person, refusing any attempt to name a different channel; verify a call naming a channel other than the one bound at creation is refused, and a successful post displays as from the webhook, not a user
- [ ] 4.3 Apply the existing message-content sanitisation path to webhook-posted content, unchanged from user-posted content; verify a webhook posting markup or script content is sanitised identically to a user doing the same
- [ ] 4.4 Implement per-webhook rate limiting on `cache.Rates` with limit-exceeded reported to the caller and visible to the webhook's owner; verify a caller exceeding the configured rate receives a clear refusal naming when to retry, and the owner can see that calls were refused
- [ ] 4.5 Record creation and revocation in the audit log with the actor, per the existing audit pattern; verify both actions appear in `GET /api/admin/audit` with the correct actor
- [ ] 4.6 Build the web interface for creating, labelling, and revoking incoming webhooks on a channel, showing the credential once at creation exactly as API tokens already do; verify the credential cannot be retrieved again after the creation response
- [ ] 4.7 Document creating and using an incoming webhook in `docs/integrating.md`, including the formatting requirements for build/alert-style content; verify the documented `curl` example posts successfully to a live server
- [ ] **MILESTONE 4** — Demonstrate: create an incoming webhook on a channel, post to it from `curl` and see the message attributed to the webhook, attempt to post to a different channel with the same credential and see it refused, revoke it and see the next call refused immediately, and find both the creation and the revocation in the audit log. Tag `v1.15.0-dev`.

## 5. Phase 5 — Applications exist as installable, scoped entities

Goal: an application is a real, listed thing distinct from a personal token — registered, approved explicitly, scoped, and removable with a choice about what it leaves behind.

- [ ] 5.1 Implement the `applications` table — name, purpose, requested permissions, external-data-flow disclosure, approval state, creator; verify a registered but unapproved application can read or write nothing
- [ ] 5.2 Implement approval as a distinct step showing the exact permission set and any external-service disclosure before granting anything; verify an administrator approving an application requesting only `channel.post` cannot make it perform an administrative action afterwards
- [ ] 5.3 Extend `apitokens.Service.Issue` to accept an application as the owning subject alongside a user, with the credential intersected against the application's own granted scope and never the approver's permissions; verify the existing user-owned-token tests are unaffected and a new application-owned-token test confirms the approver's broader permissions are not inherited
- [ ] 5.4 Implement channel/project restriction on an approved application, enforced on every request; verify an application restricted to one channel is refused when it attempts to act in another
- [ ] 5.5 Implement application-level rate limiting layered on top of the existing per-account limiting, and verify a misbehaving application is throttled without affecting other users' request latency during the same test
- [ ] 5.6 Implement the visibility endpoints — installed applications listed with permissions and approval date, and channel members able to discover which applications can read their channel; verify a channel member can find, without administrator access, which applications can read that channel
- [ ] 5.7 Implement removal — immediate credential revocation plus the operator's choice to retain or remove content the application posted, applied to the choice; verify removal ends access immediately and the content-disposition choice is applied as selected
- [ ] 5.8 Record registration, approval, and removal in the audit log with the actor; verify each appears in the audit log
- [ ] 5.9 Build the web administration interface for registering, approving, restricting, and removing applications; verify an administrator can perform every action above without calling the API directly
- [ ] 5.10 Document application registration and credential issuance in `docs/integrating.md`; verify the documented flow runs against a live server end to end
- [ ] **MILESTONE 5** — Demonstrate: register an application requesting `channel.post` scoped to one channel, approve it and see the exact permissions shown before approval, confirm its issued credential can post in that channel and nowhere else, find it listed with its permissions from an ordinary channel member's view, and remove it with content retained. Tag `v1.16.0-dev`.

## 6. Phase 6 — Bots participate, and invocations reach integrations directly

Goal: an application can act as a visible, moderatable participant, and an integration that is not holding a live connection still receives invocations reliably.

- [ ] 6.1 Implement a bot-participant identity for an approved application — displayed as a bot rather than a human, distinguishable in every client that renders a message; verify a bot's message is visually distinguishable from a human user's in both clients
- [ ] 6.2 Apply existing moderation actions (message removal, suspension, posting restriction) to a bot participant unchanged; verify a moderator suspending a bot stops it from posting exactly as suspending a human account does
- [ ] 6.3 Enforce ordinary channel-access permissions against a bot exactly as against a user; verify a bot attempting to read a channel it was not granted access to is refused
- [ ] 6.4 Implement signed invocation delivery — an integration registers a URL, and an action invocation is delivered there through the Phase 3 delivery queue rather than only as a room event; verify an integration with no live connection still receives a signed invocation within the same retry/backoff behaviour as an outgoing webhook
- [ ] 6.5 Verify authorization is evaluated against the invoking user, never the integration, on the invocation-delivery path exactly as it already is for in-room presses; verify a user without the action's required permission cannot cause a delivery to be sent at all
- [ ] 6.6 Extend `docs/integrating.md`'s worked example with a variant that registers an invocation URL instead of holding a live connection, and document the bot-participant model; verify the documented variant receives a real invocation from a running server
- [ ] **MILESTONE 6** — Demonstrate: an approved application posts as a bot distinguishable from a human, a moderator suspends it and its further posts are refused, and a second integration with no open live connection — registered only with an invocation URL — receives a signed invocation when a person presses its button, verified by running the documented worked-example variant against a live server. Tag `v1.17.0-dev`.

## 7. Cross-cutting verification

These run continuously from the phase in which they first apply, not only at the end.

- [x] 7.1 Add end-to-end tests covering the full outgoing-webhook cycle — subscribe, receive, fail, retry, disable — against a local test receiver; verify they pass on Chromium per the existing CI tier — from Phase 3
- [x] 7.2 Add a load test establishing a baseline for outbound delivery throughput and confirming a stalled receiver does not measurably affect chat message latency in the same run; verify the documented target is met and recorded — from Phase 3
- [x] 7.3 Add a test proving delivery signature verification succeeds using only the published keys and a delivery's own content, mirroring the existing federation-readiness test's independence from server-side database access; verify it passes with no database connection to the originating server — from Phase 2
- [ ] 7.4 Keep documentation current in the same change that alters the behaviour it describes; verify a behaviour change without a documentation update is rejected in review — from Phase 1
- [x] 7.5 Add server memory measurement for the webhook delivery worker pool under sustained load to the existing CI memory report; verify the figures are reported and an increase is surfaced for justification — from Phase 3
