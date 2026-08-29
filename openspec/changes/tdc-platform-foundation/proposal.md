## Why

The Tanzania Developers Community currently coordinates across Telegram and WhatsApp, with a stalled prior attempt at a developer/company directory ([`dev-space-tz`](https://github.com/Tanzania-Developers-Community/dev-space-tz), 14 commits, no code). Conversation, project work, and the developer↔client relationship are split across tools that TDC does not own, cannot moderate consistently, cannot search across, and cannot extend. Community knowledge is trapped in chat scrollback, and there is no way for a Tanzanian developer to be discovered by a client, negotiate work, and track that work in one place.

TDC needs a platform it owns: a self-hostable workspace combining team chat, project management, and a professional directory, built for the network and device reality of its members — mobile-first, on metered data, where a 4GB-RAM VPS is a real budget constraint. Because no single organisation should be a chokepoint for the community, servers must be able to interoperate, so any chapter, company, or individual can host their own and still reach everyone else.

## What Changes

This change establishes the TDC platform: repository foundation, server, web app, desktop app, and the first product surface (real-time chat).

**Delivered in this change (implementation scope):**

- **Monorepo foundation** — Bun workspaces + Go modules, versioning, CI with cross-platform release artifacts, Dockerfiles and Compose, and the documentation set (README, hosting, user, contribution, integration guides) established from the first commit rather than retrofitted.
- **Server core** — Go 1.27 + Echo v5 + Postgres, a single self-contained binary an operator can run without assembling a service mesh.
- **Identity and access** — email+password, OAuth2, and passkeys/WebAuthn; roles and granular permissions; audit logging of security- and moderation-relevant actions.
- **Real-time chat** — servers, channels/rooms, threads, direct messages, attachments with previews, link unfurling, presence and configurable status, notifications (web push + desktop), and disposable/ephemeral chats.
- **Clients** — Nuxt 4 web app (installable PWA) and a Wails v3 desktop app for Linux, macOS, and Windows, sharing one UI package.
- **Server configurability** — per-server limits, language preference, content filters, feature toggles, and theming, so an operator can shape their own deployment.
- **Signed event log** — every message and state change is a signed, append-only event. This is the substrate that makes later federation and audit possible, and it is built in from day one because it cannot be retrofitted.

**Specified but implemented in later changes:**

Project management (issues, boards, sprints), the developer/client directory and engagement workflow, federation between servers, voice/video calling, and third-party integrations/webhooks. Their specs are written now so the data model, identity scheme, and event log are designed against the full platform rather than painted into a corner.

**Key decisions made in this proposal:**

- **Licence: Apache-2.0**, chosen for adoption and integration friendliness. **BREAKING** relative to TDC precedent: existing TDC repos use GPL-3.0, so code cannot be copied from them into this one without relicensing.
- **Federation: a TDC-defined protocol**, not Matrix compliance. TDC will define a narrower protocol with home-server-authoritative rooms and signed events, taking design cues from [Buzz](https://github.com/block/buzz) (Nostr NIP-01/29, signed event log, Apache-2.0) rather than replicated-DAG state resolution. The evidence against attempting Matrix compliance is strong:
  - **Rocket.Chat** — the closest comparable product — adopted Matrix in 2022, found that "federation was almost entirely Rocket.Chat-to-Rocket.Chat" so the ecosystem payoff never materialised, and rewrote federation natively. As of Rocket.Chat 8.x (January 2026) that native federation is still beta and marked for non-critical use only, after roughly four years of funded work.
  - **Dendrite**, the Go homeserver, ran 2016–2023 and is in maintenance mode without feature parity; its last release was v0.15.2 in August 2025. Its federation and room-state code alone is roughly 39k lines, and `gomatrixserverlib` — though Apache-2.0 and usable standalone — is close to abandoned, at roughly 8 commits in the past year.
  - Matrix is a moving target: room version 12 and state-resolution v2.1 landed in September 2025, changing room IDs and creator power levels.
  - Crucially, **home-server-authoritative rooms are not a shortcut**: the IETF MIMI working group's architecture is hub-and-spoke, where one hub server owns room state and validates changes before forwarding to followers. Mattermost's Connected Workspaces and Slack Connect likewise avoid distributed state resolution. TDC's design is aligned with where the industry is going, not beneath it.
- **Zero-cost self-hosting by default.** Every dependency required to run a TDC server must be free, open-source, and self-hostable. No component of the core product may require a paid licence, subscription, or third-party API key. Paid or key-bearing services are permitted only for genuinely optional third-party integrations (AI assistance, hosted email delivery, push relays, external SSO), each behind a feature toggle, disabled by default, and documented with a free/self-hosted alternative or a clear statement that the feature is simply unavailable without it.
- **Postgres is the only datastore.** FerretDB and the DocumentDB Postgres extension were evaluated and rejected for the core product: their value is MongoDB wire-protocol compatibility, which a greenfield application does not need, while the extension is still 0.x, pins operators to Postgres 17/18, adds a gateway daemon, and shipped a regression in July 2026 breaking connections lasting over two hours — the exact profile of a chat server's long-lived connections. Document-shaped data is served by JSONB columns with GIN indexes inside the same Postgres. This keeps a small deployment at one binary plus one database, which an operator with no DBA can actually back up and upgrade.

## Capabilities

### New Capabilities

Implemented in this change:

- `platform/foundation`: monorepo layout, versioning, CI/release pipeline, Docker packaging, configuration and feature-toggle framework, documentation requirements.
- `identity/authentication`: email+password, OAuth2, passkey/WebAuthn enrolment and login, sessions, account recovery.
- `identity/authorization`: roles, granular permissions, server/channel scoping, permission evaluation rules.
- `platform/event-log`: signed append-only event model, event schema and validation, cryptographic identity, ordering and idempotency.
- `messaging/channels`: servers, channels/rooms, membership, threads, direct messages, ephemeral/disposable chats.
- `messaging/realtime`: connection lifecycle, subscription and fanout, presence and configurable status, delivery/read state, reconnection and gap recovery.
- `messaging/attachments`: upload, storage abstraction, preview/thumbnail generation, link unfurling, and the SSRF and content-safety rules governing both.
- `platform/notifications`: web push, desktop notifications, per-user and per-channel preferences, digests and mentions.
- `platform/moderation`: content filters (profanity/NSFW), reporting, moderator actions, retention.
- `platform/audit-log`: what is recorded, who may read it, tamper-evidence, retention.
- `platform/server-config`: per-server limits, locale, feature toggles, branding/theming.
- `clients/web`: Nuxt web application, PWA behaviour, offline and low-bandwidth requirements, accessibility, theming.
- `clients/desktop`: Wails desktop application, packaging and update channels, OS integration, and behaviour shared with the web client.

Specified now, implemented later:

- `federation/protocol`: server identity and discovery, request signing, room ownership, cross-server membership and message flow, trust and blocklists.
- `projects/issues`: issues, states, assignment, labels, relations, comments.
- `projects/boards`: boards, views, sprints/cycles, roadmaps.
- `directory/profiles`: developer and company profiles, skills, portfolio, availability, verification.
- `directory/engagements`: client↔developer engagement lifecycle, scoping, negotiation, agreement records.
- `calls/voice-video`: optional WebRTC calling, SFU integration, permissions and fallbacks.
- `integrations/webhooks`: inbound/outbound webhooks, event subscriptions, delivery guarantees, signature verification.
- `integrations/apps`: third-party app model, scoped tokens, bots and automation.
- `integrations/interactive-messages`: programmable messages carrying actions — buttons, menus, and inputs — that users invoke to drive integrations, in the style of Telegram and WhatsApp action buttons, with permissions evaluated against the invoking user.

### Modified Capabilities

None — this is a greenfield repository with no existing specs.

## Impact

- **Repository**: creates the entire project structure. No existing code is affected; the repo currently contains only tooling configuration.
- **Licence**: Apache-2.0 declared at the root, diverging from TDC's GPL-3.0 repos. Contributions carrying GPL code cannot be accepted.
- **Runtime dependencies**: Postgres is the only hard requirement for a single-server deployment. Object storage, a message broker, and an SFU are optional and behind interfaces, so a small deployment stays a binary plus a database. Every required dependency must be free/OSS and self-hostable; dependency licences are reviewed in CI so a licence regression fails the build rather than surfacing after release.
- **Operators**: introduces a self-hosting story TDC must support — install docs, upgrade/migration path, and backup guidance are part of the deliverable, not follow-up work.
- **Community**: TDC members must migrate from Telegram and WhatsApp. Adoption depends on the client being lighter and faster than the tools it replaces on mid-range Android phones over metered mobile data; this constrains payload sizes, bundle size, and idle resource use throughout the design.
- **Deferred risk**: federation is designed-for but not built here. The event log, identity scheme, and addressing (`user@server`, `room@server`) must be correct in this change or the later federation work becomes a rewrite.
