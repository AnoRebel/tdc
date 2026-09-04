## Context

See `proposal.md` for motivation. This document records the technical decisions, the evidence behind them, and the ones deliberately deferred.

Three constraints shape everything below:

1. **Zero-cost self-hosting.** Every required dependency must be free, OSS, and self-hostable. This eliminates otherwise-reasonable choices (MinIO, hosted push relays, commercial SFUs) and forces optional integrations behind toggles.
2. **Mobile-first on metered data.** TDC members are on mid-range Android phones paying per megabyte (roughly $0.40 for 492 MB daily, $4.05 for 4.9 GB monthly). Payload size is a product requirement, not an optimisation.
3. **Small operator, no DBA.** A TDC chapter or company should run a server on a cheap VPS. Mattermost needs roughly 4 GB for 1–50 users; a Go single binary plus Postgres should beat that substantially, and the specs commit to a 1 GB server-process budget.

The repository is greenfield, but the author has three working Nuxt 4 + Wails v3 + Go projects whose conventions this design adopts rather than re-derives: **Congrator** (`~/Code/Congrator`, web + desktop), **AnoPOS** (`~/Code/POS/AnoPOS`), and **aDex-UI** (`~/Code/Dex-UI/aDex-UI`).

## Goals / Non-Goals

**Goals:**

- A federation-ready data model and event log from the first commit, since these cannot be retrofitted.
- One shared UI codebase serving both web and desktop, with transport as the only difference.
- A deployment that is one binary plus one Postgres for a small server.
- Conventions that match the author's existing projects, so knowledge transfers both ways.
- **A working system at the end of every phase**, never a half-built one waiting on a later phase to become runnable.

**Non-Goals:**

- Matrix protocol compliance or interoperability with the existing Matrix network.
- End-to-end encryption in this change. The event model must not preclude it; implementing it is later work.
- Horizontal write scaling. A single Postgres serves the target deployment size for years.
- Feature parity with Slack, Linear, or Jira. TDC needs the subset its community actually uses.

## Decisions

### Federation: home-server-authoritative rooms over a TDC-defined protocol

**Decision.** Define a narrow protocol: domain-based server identity, discovery via a well-known path, Ed25519-signed HTTP requests, content-addressed signed events, and rooms owned by exactly one home server that is authoritative for membership, ordering, permissions, and moderation.

**Why not Matrix.** The evidence is unusually clear for a decision of this size:

- **Rocket.Chat** adopted Matrix in 2022 and reversed course. Their engineering write-up reports that Synapse "struggled to deliver production-grade performance at scale", that dual PostgreSQL+MongoDB was operationally painful, and — decisively — that **"federation was almost entirely Rocket.Chat-to-Rocket.Chat"**. The interoperability payoff that justifies Matrix's cost never materialised. Their native rewrite is still beta and marked non-critical use only in Rocket.Chat 8.x (January 2026), after ~4 years of funded work.
- **Dendrite** (Go) ran 2016–2023, is in maintenance mode without feature parity, and last released v0.15.2 in August 2025. Its `federationapi` (~13.9k LOC) plus `roomserver` (~25.6k LOC) is ~39k lines before the rest of the server.
- **`gomatrixserverlib`** is Apache-2.0 and genuinely usable standalone, but requires implementing ~15 interfaces over a real event store, and is close to abandoned (~8 commits in the past year, largely from a third party).
- Matrix is a moving target: room version 12 and state-resolution v2.1 landed September 2025, changing room IDs and creator power levels. Twelve room versions is a permanent churn tax.

**Why this is not merely a shortcut.** The IETF **MIMI** working group's architecture is hub-and-spoke: one hub server owns room state and validates all changes before forwarding to followers. **Mattermost** Connected Workspaces uses admin-brokered invitations and cursor-based batch sync. **Slack Connect** dual-writes into each org's shard with policy attached to the user's home org. None of the production comparables solve distributed state resolution. Home-server-authoritative rooms put TDC where the industry actually is.

The cost is honest and stated in the spec: a room is unavailable to remote participants when its home server is down. That is the trade we are buying tractability with.

**Concrete choices within the protocol:**

- **Signing: RFC 9421 HTTP Message Signatures**, not a bespoke scheme. It is a published RFC, Ed25519 is a recommended algorithm, off-the-shelf libraries exist, and — practically important — it survives TLS termination at a CDN or reverse proxy, which a self-hosted deployment will often have.
- **Discovery**: a well-known path returning host, port, and Ed25519 public keys, cacheable with a TTL.
- **Transport**: batched events over HTTPS with a cursor for catch-up — the same shape Mattermost ships commercially.
- **Keep the wire format swappable.** The federation layer sits behind a narrow internal transport interface. Hub-owned rooms are the decision that is expensive to retrofit; the signing scheme and encoding are a week's work to change. If cross-vendor interoperability ever becomes a genuine demand rather than a checkbox, the answer is a **bridge as a separate process**, keeping Matrix's data model out of TDC's core schema.

**On end-to-end encryption.** Deferring it is confirmed as correct by the state of the Go ecosystem: there is no credible Go MLS (RFC 9420) implementation. `cisco/go-mls` is archived, `emersion/go-mls` states it is not audited, and the remaining options are small solo projects. Serious MLS implementations are Rust and C++. Adding E2EE in Go would mean Rust FFI or commissioning an audit — real work to be planned deliberately, not absorbed into this change. The event model must simply not preclude it.

**On regulatory drivers.** EU DMA interoperability obligations bind only designated gatekeepers, which do not include team-chat products; Meta's November 2025 interop shipped as Signal Protocol over XMPP-style stanzas under bilateral contracts, not an open standard. There is no external mandate pulling TDC toward any particular protocol.

**Worth knowing: Zulip has deliberately never federated** (their issue on it has been open since 2015), substituting SSO plus a multi-server client. That is an existence proof that a successful team-chat product can skip federation entirely — which is why federation here is optional and phased rather than a precondition for shipping.

**Alternatives rejected, briefly.** XMPP has no viable Go server-to-server path (the only Go library with real s2s was archived in 2023; the actively developed one has essentially no s2s implementation), and its group-chat layer is split between a 2002 design, an abandoned successor, and a 2025 prototype. ATProto repositories are, by specification, "entirely public", and Bluesky's own direct messages are off-protocol and centralised. ActivityPub's specification openly states there is no agreed authorisation mechanism, and Mastodon's own documentation points users at Matrix for private messaging.

**The counter-argument, recorded honestly.** One line of research concluded that if federation is genuinely non-negotiable, Matrix is the only *mature* answer, since Rocket.Chat, Mastodon's docs, and Bluesky's DM design all treat it as the reference for private federated messaging. That is a fair assessment of Matrix **as a protocol design**. This design rejects it on **implementation cost against these particular constraints** — a small team, in Go, in 2026 — which is the axis on which Rocket.Chat's reversal, Dendrite's maintenance mode, and `gomatrixserverlib`'s near-dormancy are decisive. Adopting Matrix's *ideas* (hub ownership, signed events, well-known discovery) is cheap; adopting its wire protocol and replicated DAG is the trap.

**The lesson to take from ActivityPub's and ATProto's failure**: both fail precisely because they never rigorously specified authorisation. That is why `federation/protocol` specifies request authentication, cross-server membership, per-room permission enforcement, trust and blocking, and privacy boundaries as explicit requirements. "We will work out permissions later" is the one shortcut that would reproduce their failure.

**Design cues from Buzz** (`github.com/block/buzz`, Apache-2.0): every message and state change as a signed entry in one immutable log, access derived from cryptographic identity, and agents/bots as first-class members with their own keys. Buzz is explicitly single-relay rather than federated, which is precisely the shape we start from and then extend.

**Rejected: ATProto.** Its identity model (DIDs, portable repositories) is attractive, but private content is explicitly out of scope for the protocol and, per its own maintainers, "probably pretty far away". A private team workspace cannot be built on a substrate that does not do private.

### Datastore: Postgres only

**Decision.** PostgreSQL 17 or later as the sole datastore, developed and tested against 18. Normalised tables for messages, channels, memberships, issues and relations; JSONB with GIN indexes for message content blocks, custom fields and integration payloads; `tsvector` + GIN for search; `pg_trgm` for fuzzy matching.

**Why not FerretDB or the DocumentDB extension.** Both were evaluated seriously. The DocumentDB Postgres extension is MIT-licensed and Linux Foundation–governed with real multi-vendor backing (Microsoft, AWS, Google), and FerretDB is Apache-2.0 — so licensing was not the objection. The objections are:

- Their entire value proposition is **MongoDB wire-protocol compatibility**, a migration technology. A greenfield application has no Mongo drivers to preserve, so this is cost with no corresponding benefit.
- The extension is still **0.x** eighteen months in, pins operators to **Postgres 17/18**, and adds a gateway daemon. Version **0.114-0** (July 2026) shipped a documented regression **breaking connections lasting over two hours** — exactly the profile of a chat server's long-lived connections.
- FerretDB's latest release (v2.7.0, November 2025) pins DocumentDB v0.107 while upstream ships monthly and is at v0.116, suggesting the OSS proxy has slowed.
- Most importantly, **it lands on Postgres anyway** — so it is strictly more for an operator to install and understand, which contradicts the self-hosting constraint.

The data is aggressively relational (messages→threads→channels→servers, reactions as many-to-many, issues with assignees, labels, parents and cross-project relations). Denormalising into documents means a username change fans out across every message the user ever posted, unprotected by a transaction. JSONB covers the genuine flexibility need without giving up foreign keys or multi-table ACID.

### Data access: pgx with sqlc, and no ORM

**Decision.** `pgx` as the driver, `sqlc` to generate typed Go from hand-written SQL, and `golang-migrate` for schema migrations. No ORM anywhere.

This reverses the project's initial stack choice of GORM. The reasoning is long-term maintainability with outside contributors, which is the situation TDC is actually in:

- **GORM's failure modes are invisible in review.** A contributor unfamiliar with the schema can easily produce an N+1 query, an accidental full-table scan, or a silent zero-value update, and none of those fail a test or a code review — they fail in production on a small VPS. With generated SQL the query is on the page in the pull request, so a reviewer sees exactly what will execute.
- **`sqlc` catches mistakes at build time.** A wrong column name or a type mismatch fails compilation rather than at runtime. That safety net helps an unfamiliar contributor most, and is the main reason to prefer `sqlc` over `sqlx`, which still lets a typo reach production.
- **A hybrid is worse than either option alone.** The earlier plan split GORM for CRUD from raw SQL for the message hot path. In practice that obliges every contributor to learn both, plus an unwritten rule about which applies where — a boundary that would be re-argued in review and would drift. One mental model is worth more than local convenience.
- **It suits the memory target.** Predictable, inspectable queries are far easier to reason about than a reflection-heavy runtime when chasing memory use on a 1 GB budget.

**The precedent is unambiguous.** Mattermost — a Go chat server, self-hosted, with a mature open-source contributor base, and the closest analogue to this project — uses `sqlx` with `squirrel` and no ORM, having had every opportunity to adopt one. Congrator likewise uses hand-written SQL through `sqlx`.

**The honest cost.** Migrations become hand-written SQL rather than generated from struct definitions. That is more work up front, but auto-migration would have to be abandoned before production regardless: it cannot express a safe column rename or a backfill, and this product must support operators upgrading across skipped releases, which requires deterministic ordered migrations. `golang-migrate` provides those, and the migration runner already specified in `platform/foundation` assumes them.

**Implementation notes**, recorded because each cost time to discover:

- `sqlc` reads its schema from the **migration directory**, not a separate schema file, so the migrations are the single description of the database. A query naming a column no migration creates fails to generate rather than failing at runtime.
- `sqlc` singularises table names to derive struct names, and its guesses are poor for names that are already singular (`schema_meta` became `SchemaMetum`). Its `rename` option maps field names, not table-to-struct names, so the fix is to name tables in the plural.
- `golang-migrate`'s pgx driver registers itself under the scheme **`pgx5`**, not `postgres`. Operators write an ordinary `postgres://` URL and the runner translates it, rather than leaking an unusual scheme into configuration and documentation.
- Timestamps are overridden to `time.Time` rather than `pgtype` wrappers, and nullable columns to pointers, so no code outside the generated package imports `pgtype`.
- UUIDs are likewise overridden to `github.com/google/uuid`. Both the plain and the nullable override must use the **explicit `import`/`package`/`type` form**: the shorthand `go_type: "github.com/google/uuid.UUID"` emits the full path as a literal type name for the pointer case (producing invalid Go), and mixing the two forms makes sqlc emit the import twice.

### Real-time transport: server-authoritative WebSocket pub/sub

**Decision.** A WebSocket carrier with topic-based subscription, server-authoritative fanout, sequence numbers per room for gap detection, and replay from the event log on reconnect. Single-node needs nothing but Postgres; multi-node fanout uses Postgres `LISTEN`/`NOTIFY` first, with a broker interface allowing NATS or Redis later.

**Conduit is deliberately not the chat transport**, though not for the reason first recorded here. An earlier assessment said its protocol had no room or topic concept; it has since merged rooms, topics, multicast and cluster distribution, with a real `SUBSCRIBE`/`PUBLISH`/`ROOM_BROADCAST` protocol. That capability gap no longer exists.

The reasons it is still not used for chat are structural:

- **Its server is TypeScript; this server is Go.** The room and topic logic lives in the Node server, so adopting it means either running a second process — contradicting the single-binary deployment and the memory budget — or reimplementing its protocol in Go, which is more work than the hub it would replace.
- **The Go client is signaling-only**, by its own documentation. It is not a room or topic client.
- **Authorization has to be server-side and live.** This hub reads membership from the database at delivery time, which is what makes a removal take effect on an open connection. Conduit's rooms are membership-by-request, so TDC would have to check authorization before publishing regardless — leaving Conduit performing only the fan-out, which is the straightforward part.

This is not a theoretical conclusion: **`~/Code/card_games` already made this call**, using Nitro-native WebSockets (crossws) topic pub/sub as its carrier and gating Conduit behind `NUXT_CONDUIT_ENABLED=true` as an optional WebRTC enhancement. Its `server/utils/roomHub.ts` — per-recipient state redaction, connection grace timers with reconnect windows, room reaping, per-peer token bucket (20 burst / 10 per second, 16 KB max message) — is the design reference for TDC's Go hub.

**Where Conduit does belong: voice/video.** For the optional calling capability it is exactly right — P2P media with WebSocket relay fallback, MIT-licensed, already written, with a Go signaling client to pair with `pion/webrtc`.

### Calls: mesh by default, SFU behind a provider interface

**Decision.** Small calls use full-mesh P2P via Conduit, requiring **no additional infrastructure at all**. Larger calls use a selective forwarding unit reached through a narrow provider interface (mint token, create room, handle webhook).

This makes "optional" mean genuinely zero-dependency rather than merely "feature disabled", and it follows **Nextcloud Talk**, which falls back to full-mesh P2P without its high-performance backend and handles roughly five participants with video that way.

The provider interface matters because the SFU landscape is moving: **BigBlueButton 4.0 migrated from mediasoup to LiveKit** as its default media framework, and **Rocket.Chat** ships no first-party media server at all, delegating to provider apps selected by an admin setting. LiveKit (Apache-2.0, Go-native, single binary, built-in TURN) is the natural default implementation, with **Galène** (MIT) and **Jitsi Videobridge** (Apache-2.0, actively developed) as escape routes should LiveKit ever relicense. Keeping the interface narrow is what makes that hedge cheap.

Worth noting as an anti-pattern to avoid: Mattermost gates its scaling path behind paid tiers (`rtcd` requires Enterprise, group calls require Professional). That is incompatible with the zero-cost constraint. Also instructive — Mattermost ships **patched Pion forks** via `replace` directives, so even the reference "Pion in production" deployment needed to fork it. That is an argument for using a maintained SFU rather than building directly on Pion.

### Caching: in-process by default, Valkey when it earns its place

**Decision.** A `Cache` interface with two implementations: an in-process implementation that is the default and requires nothing, and a **Valkey** implementation an operator enables when they need it. A single-server deployment must not require a cache service to run.

**Valkey rather than Redis**, verified from both projects' own licence files:

- **Valkey** ships **BSD 3-Clause** (`COPYING`, "Copyright (c) 2024-present, Valkey contributors"). Permissive, Linux Foundation–governed.
- **Redis 8+** is **tri-licensed RSALv2 / SSPLv1 / AGPLv3** (`LICENSE.txt`), with the BSD terms applying only to 7.2 and earlier. Every one of those three options fails this project's rules — two are source-available rather than open source, and the third is AGPL, which the design bars as a default dependency.

Redis remains wire-compatible, so an operator who already runs Redis can point the same configuration at it. The interface targets the protocol, not the vendor, so this is the operator's choice to make and not a dependency TDC imposes.

**Where caching genuinely helps**, in rough order of value:

- Permission and role resolution, evaluated on nearly every request and changing rarely.
- Session and credential lookups on the authentication hot path.
- Channel membership sets, needed for every fanout decision.
- Unread counts and read positions, read constantly and tolerant of brief staleness.
- Link-unfurl results, which are expensive, external, and highly repetitive.
- Rate-limiter counters — the one case where a shared store is not merely an optimisation, since per-process counters let a multi-node deployment be hit N times harder than configured.

**What must not be cached**: anything where staleness is a correctness or security failure. Authorization *decisions* are computed fresh even when the role data behind them is cached, and a permission revocation must invalidate immediately rather than expire — the specs already require removal from a channel to take effect on live connections without reconnection.

**Relationship to the message broker.** Valkey can also serve multi-node fanout, but the design keeps caching and fanout as separate interfaces. Postgres `LISTEN`/`NOTIFY` remains the default fanout mechanism precisely because it needs no extra service; an operator running Valkey for cache may use it for fanout too, and that is a configuration choice rather than a coupling.

**Memory implications.** In-process caching consumes the server's own budget, so entries must be bounded by count or size with eviction, never unbounded maps. This is also an argument for the Valkey path at scale: it moves cache memory out of the server process, where the budget is tightest.

### Interactive and programmable messages

**Decision.** Messages can carry actions — buttons, menus, short inputs — that integrations post and users invoke, in the style of Telegram and WhatsApp automated messages with action buttons. This is specified separately from webhooks (`integrations/interactive-messages`) because it is not merely an outbound event: it requires callbacks, per-element state, and — critically — permission evaluation against **the user who clicked**, not the integration that posted.

The security-relevant decisions: invocations are signed by the server like any other outgoing delivery; the invoker's identity is taken from their authenticated session and cannot be set by client or integration; single-use and expiry are enforced server-side so integrations need not defend against replay themselves; updates replace a message's controls atomically so a decision cannot be recorded twice through a stale view; and any resulting state change is attributed in the audit log to the person, noting the integration through which it was made.

### Maps and location

Should a capability need maps (for example, location on a company profile or an in-person event), the author hosts a map service at `mapcn.anorebel.net`. No capability in this change requires maps, so this is recorded as an available option rather than a dependency — and any future use must sit behind a feature toggle so a server that does not want an external map dependency can run without one.

### Clients: shared Nuxt layer with per-transport adapters

**Decision.** Adopt Congrator's proven model directly:

- A shared package (`packages/ui`) published as a **Nuxt layer** (`"main": "./nuxt.config.ts"`, consumed via `extends:`), holding components, composables, schemas and types.
- A **transport-agnostic `ApiClient` interface** in that layer. Nothing under it may import a transport primitive.
- Per-app adapters: `$fetch` for web, Wails bindings for desktop.
- Wired by Vue `provide`/`inject` under an injection key — not `useNuxtApp` — so the layer stays framework-core-only and unit-testable against a mock adapter.

Two things provably cannot be shared, both learned the hard way in Congrator and both to be replicated here: `nuxt-i18n-micro` configuration must live in the consuming app (a value in the layer bakes in as the module default and 404s every locale payload), and `@nuxt/icon`'s client-bundle scanner needs an explicit `globInclude` for the layer's components, since `node_modules` is glob-excluded by default (`nuxt/icon#358`).

### Frontend stack

Matching the house conventions, with versions verified as of August 2026:

- **Nuxt 4.5.2**, `ssr: false`, `future.compatibilityVersion: 5`. Nuxt 5 is expected Q4 2026 once Nitro 3 stabilises; the compatibility flag is the supported way to adopt its behaviour early and is already in use in AnoPOS and Congrator.
- **Tailwind v4** via `@tailwindcss/vite`.
- **Nuxt UI 4.11** for general components — note that Nuxt UI Pro is now merged into it, so there is no commercial licence for an open-source project to worry about.
- **shadcn-vue DataTable rather than Nuxt UI's `UTable`.** Nuxt UI 4.11 depends on `@tanstack/vue-table ^8.21.3`, while TanStack Table v9 is now GA at 9.2.4. Using `UTable` would either pin us to v8 or risk two copies of `table-core`. shadcn components are vendored into the repository, so we control the TanStack version and can modify the component. AnoPOS already runs Nuxt UI and shadcn-nuxt together, so this is an established combination.
- **shadcn-vue community registries** are worth drawing on rather than building from scratch, installed via the CLI and then vendored like any other shadcn component. **`@inspira-ui`** (animated components and effects) is the primary one to draw on for the interface polish the product needs. **`@ai-elements`** (conversation and message primitives) is intended for the AI-integration surface — see below. **`@mapcn`** is the author's own map registry, the natural route for any future location feature, making maps a first-class component rather than an external embed. Because these are community-maintained, review the code at install time; vendoring means we own whatever we take.

### AI participants

Not implemented in this change, but the design deliberately accommodates it, because retrofitting it would be expensive. **Buzz's central idea is that AI agents are first-class members** — they hold their own cryptographic keys and channel memberships, and operate with the same audit trail and surface area as human teammates, rather than being a bolted-on assistant.

TDC's model already supports this without modification: the event log gives every author a cryptographic identity; `integrations/apps` specifies bots with their own credentials, scoped permissions, rate limits, and full moderatability; `integrations/interactive-messages` gives an agent a way to ask a person for a decision and act on the answer; and `platform/audit-log` records what an agent did and on whose behalf. An AI participant is therefore an application with an identity, not a special case in the data model.

Two constraints carry over from the zero-cost principle: any AI integration requiring an external API key must be optional, disabled by default, and documented with what data leaves the server (per `integrations/apps`), and a server with no AI integration configured must remain fully functional. `@ai-elements` supplies the conversation UI for this surface when it is built.
- **TanStack Form** (`@tanstack/vue-form` 1.33.5) with **Valibot** via Standard Schema. Note the Vue adapter lacks React's `createFormHook`/`useFieldGroup`, so field components are wired manually.
- **TanStack Charts** (`@tanstack/charts` + `@tanstack/charts/vue`) as the default charting library, per the author's direction. It is real and actively developed, but **alpha** — 30 releases in 26 days, with breaking changes between minors and an explicit instruction to pin exact versions. It is therefore used **behind a thin chart-adapter interface**, so falling back to `@unovis/vue` or `vue-chrts` is a configuration change rather than a rewrite. Pin the exact version.
- **VueUse**, plus **`@vueuse/motion`** for animation and **`@vueuse/gesture`** for touch gestures (swipe-to-reply, swipe between channels, pull-to-refresh). Note for CSP: `@vueuse/motion` writes inline style *attributes*, which hashes cannot cover, so `'unsafe-inline'` must remain in `style-src` — as documented in Congrator.
- **date-fns** with `@date-fns/tz`.
- **`nuxt-i18n-micro`**, English and Swahili. AnoPOS already ships `sw-TZ`.
- **`v-tour-guide`** for onboarding tours — a Vue 3 + TypeScript library with Nuxt support and a `useTourGuide` composable. Onboarding matters disproportionately here because users are arriving from Telegram and WhatsApp.

### PWA

**Decision.** `@vite-pwa/nuxt` (pinned; 1.1.1 is the version proven in `card_games`), lifting that project's configuration as the baseline — install and update prompts, icons generated reproducibly from a single SVG, and critically its `workbox.navigateFallbackDenylist: [/^\/api\//, /^\/_ws/]`, since a chat app has the identical hazard of the service worker swallowing real-time paths.

Two gaps must be filled, because `card_games` did not need them:

- **Web push does not exist there at all** — no VAPID, no push handler, no subscription storage. Adding it **forces a switch from `generateSW` to `injectManifest`**, since a `push` listener cannot be added to a generated service worker. This is real work, not configuration.
- **Runtime caching and a genuine offline fallback**, since chat has no fully-offline mode the way a card game does.

Known constraint to document: **web push on iOS works only for home-screen-installed PWAs**, and not at all for EU users since Apple removed standalone PWA support there under the DMA in iOS 17.4.

A known risk with `@vite-pwa/nuxt` under Nuxt 4: it declares `@nuxt/kit: ^3.9.0`, a caret range that cannot match Nuxt 4's 4.x kit, so the tree carries two copies of `@nuxt/kit`. That duplicate is the mechanism behind a cluster of reported `srcDir`/`app/` path bugs. Whether these are resolved in 1.1.1 must be verified during implementation, since `card_games` runs this combination successfully.

### Desktop: Wails v3

**Decision.** Wails v3 with `build/config.yml` (v3 has no `wails.json`), version single-sourced from that file, `//go:embed all:frontend/dist`, bindings generated into `frontend/bindings/` and aliased since they sit outside Nuxt's `app/` srcDir, dev server on port 9245 from `WAILS_VITE_PORT`.

**Pin the exact Wails version.** v3 is at `v3.0.0-beta.15` and its beta tags are **automated nightly builds off master**, not curated milestones. The GA tracker reports 38 open gates, 27 blocking, zero passed. Congrator already pins exactly, with a written rationale; TDC does the same, keeping a known-good version to roll back to. Keep the Go module and `@wailsio/runtime` in lockstep — mismatched pairs are not guaranteed to work.

**Linux is the real risk, and it is not escapable by switching frameworks.** Wails v3 now defaults to GTK4 + WebKitGTK 6.0, with `webkit2gtk-4.0` gone and the `-tags gtk3` path scheduled for removal in v3.1. Tauri carries 64 open WebKitGTK issues of its own, so the rendering risk is inherited from the platform, not the framework; Wails is arguably ahead, since it auto-applies the NVIDIA DMA-BUF workaround in `init()`. Follow Congrator's practice of building Linux twice (GTK4 default plus a time-limited gtk3 build for Ubuntu 22.04 / Debian 12 / RHEL 9), and test early on NVIDIA under both Wayland and X11.

**Why a desktop app at all,** given the web app is a PWA: OS keychain credential storage (materially better than browser storage for long-lived tokens), system tray with unread indication, global shortcuts, launch at login, and reliable background notifications. Those justify it; "Go is fast" does not, since a chat client is network-bound.

### Backend

Go 1.27 (released 19 August 2026), Echo v5.3.1, `go-webauthn/webauthn` for passkeys, `alexedwards/argon2id` for password hashing — all already in use in Congrator or AnoPOS. Project layout follows aDex-UI's `internal/` + `pkg/` + `cmd/` convention.

**Object storage** is accessed through an interface with a local-filesystem default, so a small deployment needs no object store at all — which is what Mattermost and Zulip both do.

**The MinIO server is excluded.** It removed the web console from the community edition and moved features behind an enterprise licence, and the server repository was archived in April 2026 carrying an unpatched high-severity vulnerability. Recommending it to self-hosters would be irresponsible, quite apart from the zero-cost constraint. Note the distinction: the MinIO **server** is what is excluded; S3 **client** libraries are unaffected.

**Both S3 clients ship, behind the interface.** An earlier revision of this document preferred `minio-go` on the grounds that `aws-sdk-go-v2` signed `amz-sdk-request` into `X-Amz-SignedHeaders`, breaking plain browser redirects to presigned URLs — exactly the path used to serve attachment downloads. That reason no longer holds: the behaviour was a cross-module version mismatch, fixed upstream in February 2024. Verified against the current SDK, `PresignGetObject` signs only `host`, which a browser sends itself, so a plain redirect works.

With the deciding argument gone, both are Apache-2.0, actively maintained, and satisfy the interface. Shipping both costs one small adapter each and removes the need to relitigate this: an operator already running the AWS SDK elsewhere uses it, and one who prefers the smaller surface uses `minio-go`. The local filesystem remains the default, so most deployments use neither.

Operators wanting object storage are pointed at **Garage**, whose single-node mode targets precisely this audience, or **SeaweedFS**. Note that **Garage is AGPL-3.0**, which is compatible with the rule below because it is a *documented operator choice* — a separate program the operator runs and reaches over a public multi-vendor network protocol, not a library TDC links or a default we impose. The rule bars AGPL from the **default path**, not from an operator's menu.

**Search: Postgres full-text only, behind a `Searcher` interface.** Both Zulip and Synapse run plain Postgres FTS at millions of messages, and Synapse's implementation is a working reference to follow. Three implementation details that follow from the research and should not be rediscovered:

- Use `websearch_to_tsquery`, which never raises a syntax error on hostile input and gives quoted phrases, `or`, and `-negation` for free.
- **Sort by recency, not `ts_rank`.** Chat messages are short, so every term appears once and relevance ranking degenerates — and recency is what users actually want.
- `websearch_to_tsquery` does **not** support prefix matching, so autocomplete needs a separate `pg_trgm` path, which also buys typo tolerance.

Put search text in its own table as Synapse does, so the index can be rebuilt without touching the messages table. Default CJK tokenisation is genuinely broken rather than merely weak; PGroonga is the opt-in fix and remains a mere extension, adding no service.

**Bleve is explicitly rejected**, correcting an obvious-looking choice for a Go project: **Mattermost retired Bleve in Server v11.0 (October 2025)** — the largest Go chat server, with this exact architecture, ran embedded Bleve for years and deleted it. Embedding a search index means a second write path, a second backup, index/database divergence, and rebuild storms, all inside the server process where they take it down.

**Media processing and link unfurling run in a separate, egress-restricted container.** For a self-hostable product, shipping a second container is far easier than asking operators to keep an image library patched. Use pure-Go imaging with `DecodeConfig`-first pixel caps rather than libvips. For SSRF protection, use a maintained, IANA-registry-driven library in `net.Dialer.Control` — **never hand-roll an address deny-list**: Rocket.Chat shipped resolve-then-check and still lost to a time-of-check/time-of-use race.

**A standing rule that fell out of this research: nothing AGPL-licensed may be a default dependency.** It may be a documented option (Garage, ParadeDB's `pg_search`), but the default path must stay permissive so operators face no licence question to run TDC.

### Incremental delivery: every phase ships something that runs

**Decision.** The work is sequenced so each phase ends in a demonstrable, running system rather than a layer that only becomes useful later. Each phase closes with a milestone that must be satisfiable by launching the software and using it — not by a green test suite — and each is tagged.

This is why the task breakdown is ordered by **user-visible capability** rather than by architectural layer. A layer-first plan (all models, then all services, then all endpoints, then a UI) leaves nothing runnable until the end and hides integration problems until they are expensive. Instead: Phase 1 runs and stores data, Phase 2 admits real users, Phase 3 is the first genuinely usable chat, and so on.

Three consequences worth stating, because they shape implementation choices:

- **Some work is deliberately done twice.** The web app talks to the server directly in Phases 1–8, and only in Phase 9 is the shared `ApiClient` layer extracted, when the desktop client gives it a second consumer. Extracting the abstraction earlier would mean designing an interface against one hypothetical caller. The Phase 9 refactor is explicitly gated on not regressing the Phase 8 milestone.
- **Later phases must not break earlier ones.** Each milestone remains a standing regression target; the cross-cutting end-to-end tests exist from the phase where they first apply and grow with each subsequent one.
- **Hard, unglamorous work is not deferred to the end.** Signed events (Phase 3), SSRF protection (Phase 5), and authorization enforcement (Phase 2) land with the features they protect, not in a hardening phase afterwards — retrofitted security is the failure mode this ordering exists to avoid.

The one thing deliberately front-loaded regardless of visible payoff is the **event model**, because it is the single component that cannot be retrofitted once data exists.

### Testing and CI

Vitest with `@vue/test-utils` and happy-dom for unit tests; Go's standard testing with `-race`; **Playwright as the cross-browser CI backbone**.

On **Bunwright**: it is a real project and it is already the E2E driver in all three of the author's existing repositories (`bunwright ^0.3.2`). It is however early (41 stars, ~93 commits, placeholder website) and drives Bun's WebView — one engine, not Chromium/Firefox/WebKit. Since TDC must gate versioned releases on browser compatibility, Playwright provides the cross-browser matrix and Bunwright is retained for fast local and Bun-native runs. This keeps the house tool without betting release gating on a very young project.

CI follows the established pattern: per-workspace jobs, a `ci-success` aggregate gate, pinned Go and Wails versions with written rationale, dependency licence checks, vulnerability scanning, migration checks against the previous release's schema, and reproducible builds using `SOURCE_DATE_EPOCH` from the commit timestamp.

**CI is tiered by branch.** `dev` is the fast-moving integration branch and deliberately runs a light gate; `beta` is the pre-release gate; `main` is the release branch.

- **Every push, any branch, including `dev`** — lint, format, typecheck, unit tests, and a single-platform build. These must stay quick enough to run on every commit; they are what a developer waits on, and keeping `dev` light is the point.
- **`beta` and `main`, and pull requests targeting them** — the full matrix: cross-browser end-to-end tests, multi-platform builds, migration validation against the previous release's schema, dependency licence and vulnerability scanning, accessibility checks, payload-budget checks, memory measurement, load tests, and desktop packaging verification. This is the `ci-success` gate governing merge into either.
- **Tags only** — release artifact production, signing, and publication.

The rule of thumb: a check that guards a *release* belongs on `beta` and `main`; a check that guards a *commit* belongs everywhere. The trade being accepted is that a defect only the full matrix catches can land on `dev` and be found when it is promoted to `beta` — which is the right place to absorb it, since `dev` is explicitly the integration branch and not a release surface.

### Linting and formatting

**Decision: ESLint flat config with Prettier. Not Biome, and not both.**

Biome is the faster tool and would otherwise be attractive as a single binary replacing both. It is rejected because **Biome's own language-support matrix marks Vue as experimental for parsing, formatting, *and* linting** — not merely incomplete at the template level, but experimental end to end. For a codebase whose frontend is predominantly `.vue`, that is an experimental dependency on the primary file type. Biome's JS/TS/JSON/CSS support is fully mature, so a split (Biome for those, ESLint for `.vue`) is technically viable, but it means two formatters in one repository, and most of the frontend code here is `.vue` anyway — so the speed gain would apply to the minority of files while creating a standing risk of two configurations disagreeing.

ESLint plus Prettier also matches all three of the author's existing projects, so configuration and muscle memory transfer directly. The stack is `eslint.config.mjs` (flat config) with `@nuxt/eslint`, `eslint-plugin-vue` for template linting, and `eslint-plugin-vuejs-accessibility`, alongside `vue-tsc` for type checking and `.golangci.yml` for Go.

**Style: semicolons, double quotes.** Configured in Prettier (`semi: true`, `singleQuote: false`) and enforced in CI, so it is settled by tooling rather than argued in review.

This decision is worth revisiting once Biome's Vue support leaves experimental; the linting configuration should stay simple enough that switching is not a large undertaking.

### Go and JavaScript tooling

**Go**: `golangci-lint` for linting, `air` for live reload during development, and the standard toolchain for everything else. Development tooling is pinned in the repository so contributors get the same versions.

**Bun 1.4** is the package manager, with three behaviours confirmed by testing in this repository rather than assumed:

- **Workspace dependencies are not hoisted to the root.** They install into the package's own `node_modules` (`packages/types/node_modules/vitest`, not `node_modules/vitest`). Any script or tool that looks for a dependency at the root will not find it; resolve from the consuming package instead.
- **Every entry in the `workspaces` list must exist**, or `bun install` fails outright rather than skipping the missing one. The list therefore grows as apps are added, rather than being written ahead of them.
- **Directories beginning with a dot are ignored** by workspace globs.

Cross-workspace resolution through a package's `exports` map works correctly for both type checking and runtime import, verified with a throwaway consumer package before anything was built on top.

**Versions are centralised with Bun catalogs.** Shared dependency versions live once in the root `package.json` under `workspaces.catalogs` (grouped as `vue`, `tooling`, `testing`, `utils`) and each package references `catalog:<group>`. This removes the drift that comes from repeating `vue`, `typescript` and `vitest` ranges across every package — a real shortcoming in the reference projects, where the same tool is pinned to different versions in different packages. Note the one limitation: `catalog:` resolves only in root and workspace manifests, which is fine here since every package is private and unpublished.

**TypeScript is split by tool.** The root and non-Vue packages use **TypeScript 7**; packages type-checked by `vue-tsc` stay on **TypeScript 6** until `vue-tsc` supports 7. The catalog makes this explicit: `tooling.typescript` is the Vue-compatible version, and packages that need 7 state it directly.

**The application version is centralised too.** A single `VERSION` file at the repository root is the source of truth, propagated by `scripts/sync-version.ts` into every manifest, the Wails `build/config.yml`, and the Go `buildinfo` package. CI runs it with `--check`, so a hand-edited version in one file fails the build instead of shipping artifacts that disagree about what they are.

### Commit messages and documentation

**Decision.** Commit messages and documentation describe **what changed and why, in terms of the software** — not the process that produced them. They should not reference internal task numbers, specification identifiers, planning artifacts, or the involvement of AI tooling. A reader of the history a year from now cares what the code does, not how the work was scheduled.

The exception is where such a thing is genuinely part of the product: documenting an AI *integration* the software offers is a user-facing feature and belongs in the documentation like any other.

**Contributions produced with AI assistance are welcome, and must be disclosed.** The contribution guide states that a contributor who used AI assistance should say so, and that every pull request must carry a rationale explaining what it changes and why. The disclosure exists so reviewers can calibrate scrutiny; the rationale requirement applies to every contribution regardless of how it was written, because an unexplained change is hard to review whatever produced it.

### Deployment

Docker image plus a Compose definition that works unedited for evaluation. **Dokploy** is the intended host for the public demo instance, and this repository already has a Dokploy MCP server configured — so the demo deployment can be driven from here once there is something to deploy.

## Risks / Trade-offs

- **Scope is very large, and TDC's previous attempt at a subset stalled at 14 commits with no code.** → Implementation is deliberately limited to foundation + chat, with specs written for the whole platform so the data model is right. Success is measured by TDC actually leaving Telegram and WhatsApp, not by feature count.
- **Wails v3 is beta with nightly release tags and 27 blocking GA gates.** → Pin exact versions, keep a known-good rollback, treat desktop as the second-priority client behind the PWA. If v3 proves unstable, the PWA still delivers the product.
- **Linux WebKitGTK rendering bugs.** → Test on NVIDIA under Wayland and X11 early; build both GTK4 and gtk3 variants; the DMA-BUF workaround is applied automatically by Wails.
- **TanStack Charts is alpha with breaking changes between minors.** → Pin exactly; place behind a chart-adapter interface so a swap to Unovis is configuration, not rewrite.
- **`@vite-pwa/nuxt` duplicate-kit path bugs under Nuxt 4.** → Verify against 1.1.1 early; `card_games` runs the combination successfully, so a working configuration exists to copy.
- **Web push is genuinely new work** requiring `injectManifest`, and is unavailable on iOS outside installed PWAs and entirely in the EU. → Treat push as an enhancement; in-app and desktop notification paths must work without it.
- **Federation is designed-for but unbuilt.** If the event model, addressing, or signing is wrong, the later work becomes a rewrite. → The event-log spec makes federation-readiness testable now: a foreign event must be verifiable using only the originating server's published keys and the event's own content.
- **Home-server-authoritative rooms mean a room dies with its home server.** → Accepted deliberately, documented in the spec, and the price of avoiding distributed state resolution.
- **A single Postgres is a single point of failure and a write ceiling.** → Correct for the target deployment size; backup and restore are part of the hosting guide, and the broker interface leaves room to grow.
- **The 1 GB memory target may conflict with a better-engineered choice.** → It is a target, not a gate. CI measures and reports memory on every change with the delta from baseline, so an increase is always visible; exceeding the target requires a recorded justification rather than blocking the change. Correctness, security, and maintainability win where they genuinely conflict.
- **In-process caching consumes the server's own memory budget.** → All in-process caches are bounded with eviction, never unbounded maps. The Valkey path exists partly to move cache memory out of the process where the budget is tightest.
- **Cached authorization data could outlive a revocation.** → Authorization decisions are computed fresh even when the underlying role data is cached, and revocation invalidates rather than waiting for expiry. The specs already require removal from a channel to take effect on live connections without reconnection, which makes this testable.
- **A defect only the full CI matrix catches can land on `dev`.** → Accepted deliberately to keep the integration branch fast; it is caught on promotion to `beta`, which is what that gate is for.
- **Bun 1.4's workspace resolution differs from npm's** and the shared UI layer depends on cross-workspace resolution working correctly. → Follow Congrator's proven arrangement rather than inventing one, and verify clean-clone resolution as an explicit Phase 1 task before anything is built on top.

## Open Questions

These can be answered during implementation without changing the specs or the task breakdown:

- Exact wire format for federated events (a compact binary encoding versus signed JSON) — the spec constrains the properties, not the encoding.
- Whether presence propagates across federated servers at all, or stays local. Cross-server presence is expensive and often low-value; the spec does not require it.
- Whether message search is exposed across federated rooms or only local ones.
- The precise memory and payload budget figures to enforce in CI — the specs require budgets to exist and be enforced; the numbers get set once there is something to measure.
