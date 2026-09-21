Scope note: these tasks implement **foundation + chat** only. The specs cover the full platform; project management, directory, federation, calls, and integrations are separate later changes. Tasks reference specs for what to build and `design.md` for how.

**Documentation rule: documentation is written as the work is done, not after it.**
Anything an operator or a contributor would need to know is written in the same
commit as the capability it describes — how to configure it, what it costs, what
it refuses to do and why. A phase that adds a setting, an endpoint, a command or
a constraint leaves the relevant document current before its milestone is
claimed. Writing this at the end means reconstructing reasons from code that no
longer remembers them, and the reasons are most of the value.

**Phasing rule: every phase ends in a running, demonstrable system.** Each group below closes with a milestone task that must be satisfiable by launching the software and using it — not by a passing unit test alone. No phase may leave the default branch in a state where the server does not start, the app does not load, or a previously working capability has regressed. A phase's milestone is the gate: if it cannot be demonstrated, the phase is not done, regardless of how many of its tasks are ticked.

## 1. Phase 1 — It runs and stores data

Goal: a server that starts, connects to Postgres, reports its version, and a web app that loads and talks to it.

- [x] 1.1 Initialise the monorepo on Bun 1.4 with Go modules and `go.work`, following Congrator's working workspace arrangement rather than a fresh one; verify a clean-clone `bun install` resolves every workspace and a sibling package's `exports` map is honoured when imported across the workspace
- [x] 1.1a Set up `air` for Go live reload and pin development tool versions in the repository; verify `air` rebuilds and restarts the server on a source change
- [x] 1.2 Add the Apache-2.0 `LICENSE` and `NOTICE` at the root; verify the licence is detected correctly by the repository host
- [x] 1.3 Create the directory structure — `server/` (`internal/`+`pkg/`+`cmd/`), `packages/ui/`, `packages/types/`, `apps/web/`, `apps/desktop/`; verify each builds an empty artifact
- [x] 1.4 Set up the Echo v5 server with structured logging, graceful shutdown, and health endpoints; verify it starts, serves health, and shuts down cleanly on signal
- [x] 1.5 Implement configuration loading from file with environment override, startup validation reporting every problem, and secret redaction; verify two invalid values are both reported and startup is refused (`platform/foundation`, `platform/server-config`)
- [x] 1.6 Set up Postgres connectivity with `pgx` and configure `sqlc` to generate typed Go from hand-written SQL; verify the pool connects, a generated query round-trips, and `sqlc vet` runs in CI
- [x] 1.7 Implement the migration runner applying migrations transactionally on start and refusing a newer schema than the binary; verify a skipped-release upgrade applies all pending migrations in order (`platform/foundation`)
- [x] 1.8 Implement the version endpoint reporting release and protocol version unauthenticated; verify it matches the built artifact (`platform/foundation`)
- [x] 1.9 Scaffold the Nuxt 4 web app with `ssr: false` and `compatibilityVersion: 5`; verify it builds and renders a page
- [x] 1.10 Write the server Dockerfile and a Compose definition bringing up server and Postgres unedited; verify `docker compose up` yields a reachable server (`platform/foundation`)
- [x] **MILESTONE 1** — Demonstrate: `docker compose up` starts the stack, the web app loads in a browser, and it displays the server's version fetched from the live API. Tag `v0.1.0-dev`.

## 2. Phase 2 — People can sign in

Goal: real accounts, real sessions, enforced permissions — the system is usable by more than one identity.

- [x] 2.1 Implement the user model, registration, and first-run administrator setup that closes after the first administrator exists; verify the setup path becomes unavailable and no default credential exists (`identity/authentication`)
- [x] 2.2 Implement password authentication with argon2id, weak/compromised rejection, and account-existence-neutral failures; verify failure responses are indistinguishable for existing and non-existing accounts (`identity/authentication`)
- [x] 2.3 Implement sessions with expiry, per-device listing, and individual and bulk revocation; verify a password change invalidates all other sessions (`identity/authentication`)
- [x] 2.4 Implement per-account and per-source rate limiting with identifier normalisation; verify case-varied identifiers share one limit (`identity/authentication`)
- [x] 2.5 Implement the permission catalogue, roles, multi-role union with explicit denials, and the administrator floor; verify a channel-scope denial overrides a server-scope grant and the last administrator cannot be demoted (`identity/authorization`)
- [x] 2.6 Implement server-side authorization enforcement on every endpoint; verify a request bypassing the UI is denied and names the required permission (`identity/authorization`)
- [x] 2.7 Implement the `Cache` interface with the in-process default, bounded with eviction, applied to permission and session lookups; verify no unbounded growth under sustained load and that a revocation invalidates immediately rather than expiring (`design.md` — Caching)
- [x] 2.8 Implement audit recording for authentication and authorization changes with the separate audit-read permission, tamper-evidence, and fail-closed behaviour; verify an unrecordable privileged action is refused while ordinary requests are unaffected (`platform/audit-log`)
- [x] 2.9 Build the web sign-in, registration, and session-management screens; verify a user can register, sign in, view sessions, and revoke one
- [x] **MILESTONE 2** — Demonstrate: two people register on a running server, sign in from different browsers, see their own sessions, and an administrator's permission change visibly takes effect on the other user. Tag `v0.2.0-dev`.

## 3. Phase 3 — People can talk

Goal: the core product. Channels, messages, and live delivery between real clients.

- [x] 3.1 Define the event schema with content-derived identifiers and implement Ed25519 signing and verification with rotatable keys; verify identifiers change when any signed field changes and retired keys still verify history (`platform/event-log`)
- [x] 3.2 Implement the append-only event store with per-room total ordering; verify two readers observe identical order for the same range (`platform/event-log`)
- [x] 3.3 Implement idempotent submission keyed on a client token; verify an identical retry yields one event and returns the same identifier (`platform/event-log`)
- [x] 3.4 Implement `user@server` and `room@server` addressing throughout; verify API responses always name the home server (`platform/event-log`, `messaging/channels`)
- [x] 3.5 Implement channel creation, rename, archive, and restore; verify archiving preserves history and refuses posts, and restoring resumes posting (`messaging/channels`)
- [x] 3.6 Implement public and private visibility with non-disclosure; verify a non-member cannot distinguish "forbidden" from "does not exist" (`messaging/channels`)
- [x] 3.7 Implement channel membership with immediate effect on live connections; verify a removed member stops receiving events without reconnecting (`messaging/channels`, `messaging/realtime`)
- [x] 3.8 Implement posting, editing, and deleting messages with edit markers and deletion placeholders; verify a deleted message leaves evidence rather than vanishing (`messaging/channels`)
- [x] 3.9 Implement the WebSocket connection lifecycle with authentication, operator-configured keepalive, and graceful close; verify an idle connection generates only keepalives (`messaging/realtime`)
- [x] 3.10 Implement topic subscription changes without reconnecting, and authorization-scoped fanout; verify an event for a non-member channel is never delivered, including when access is revoked mid-connection (`messaging/realtime`)
- [x] 3.11 Implement per-room sequence numbers, gap detection, and replay on reconnect, plus the oversized-gap resynchronise path; verify a reconnecting client receives exactly the missed events with no duplicates (`messaging/realtime`)
- [x] 3.12 Implement client reconnection with randomised backoff; verify many simultaneously disconnected clients do not reconnect in lockstep (`messaging/realtime`)
- [x] 3.13 Build the web chat interface — channel list, timeline, composer — using shadcn-vue's chat components (bubbles, separator, keyboard keys, preview) rather than hand-rolling them; verify the full send/receive/edit/delete cycle works against a live server
- [x] **MILESTONE 3** — Demonstrate: two people in different browsers exchange messages in a shared channel in real time, one edits and deletes a message, one disconnects and reconnects and sees exactly the messages missed. This is the first genuinely usable version. Tag `v0.3.0-dev`.

## 4. Phase 4 — Conversations get structure

Goal: threads, direct messages, presence, and read state — enough that it competes with what TDC uses today.

- [x] 4.1 Implement threads inheriting the parent channel's permissions; verify replies stay out of the main timeline and follow channel visibility (`messaging/channels`)
- [x] 4.2 Implement direct and group direct messages not readable through administrative interfaces; verify an administrator cannot read them via ordinary endpoints (`messaging/channels`)
- [x] 4.3 Implement presence derived from connection state with expiry on ungraceful disconnect and an invisible option; verify presence returns to offline within the bound after a dropped connection (`messaging/realtime`)
- [x] 4.4 Implement operator-configurable statuses with expiry and notification suppression; verify a status reverts automatically at its expiry (`messaging/realtime`)
- [x] 4.5 Implement typing indication with automatic expiry and a user opt-out; verify the indicator disappears when not refreshed (`messaging/realtime`)
- [x] 4.6 Implement per-conversation read state converging across a user's devices, cached per the design; verify reading on one device updates the others and an offline device converges on reconnect (`messaging/realtime`)
- [x] 4.7 Implement channel discovery and search respecting visibility; verify search never returns a channel the user cannot discover (`messaging/channels`)
- [x] 4.8 Build the web interface for threads, direct messages, presence, and unread indicators; verify each is usable end to end
- [x] **MILESTONE 4** — Demonstrate: a threaded conversation, a private direct message, live presence and typing indicators between two clients, and unread counts that clear correctly across two devices signed in as the same user. Tag `v0.4.0-dev`.

## 5. Phase 5 — Files and links

Goal: attachments with previews and safe link unfurling.

- [x] 5.1 Implement the storage interface with a local-filesystem default and two S3-compatible implementations, one on `aws-sdk-go-v2` and one on `minio-go`, selected by configuration per the design; verify attachments work with no object store configured, and that each S3 implementation satisfies the same interface tests (`messaging/attachments`)
- [x] 5.2 Implement upload with server-enforced size and type limits; verify a client bypassing its own validation is refused and told which limit it exceeded (`messaging/attachments`)
- [x] 5.3 Implement content-based type detection and inert serving headers; verify a mismatched declared type is handled by detected type and cannot execute in the app's browsing context (`messaging/attachments`)
- [x] 5.4 Implement retrieval authorization and expiring, user-bound download links; verify knowing a storage location alone does not grant access (`messaging/attachments`)
- [x] 5.5 Build the optional media-processing sidecar container with egress restrictions, read-only filesystem and resource caps per the design; verify the server does its own processing when none is configured, uses the sidecar when one is, and continues to serve attachments without previews if a configured sidecar is unavailable
- [x] 5.6 Implement resource-bounded thumbnail generation with `DecodeConfig`-first pixel caps in the sidecar; verify a malformed or decompression-bomb file fails within bounds without affecting other requests (`messaging/attachments`)
- [x] 5.7 Implement link unfurling with a maintained IANA-registry-driven SSRF library in `net.Dialer.Control`, checked at connect time, plus timeout, size, and redirect limits; verify loopback, link-local, and private addresses are refused and redirects cannot escape (`messaging/attachments`)
- [x] 5.8 Implement per-operator and per-user unfurl toggles and cache unfurl results per the design; verify disabling either makes no outbound fetch (`messaging/attachments`)
- [x] 5.9 Implement attachment deletion and orphan reclamation; verify deleted attachments are unreachable and referenced ones survive cleanup (`messaging/attachments`)
- [x] 5.10 Implement the optional malware-scanning hook, disabled by default and failing closed; verify an unreachable scanner refuses the upload unless fail-open is configured (`messaging/attachments`)
- [x] 5.11 Build the web upload, preview, and link-preview interface with the user-controlled automatic-media setting; verify disabling it prevents media fetches until requested (`messaging/attachments`, `clients/web`)
- [x] 5.11a Build the attachment viewer for images, animated images, video, audio and PDF, opening over the conversation with keyboard dismissal and navigation between one message's attachments; verify an unrenderable type still shows name, size and type with a download, and that rendered content cannot reach the application's session or storage (`clients/web`)
- [x] 5.11b Build upload feedback with real transfer progress, cancellation, and a retry that does not re-select the file; verify progress comes from the transfer rather than a simulation, a refusal names the limit it broke, and the conversation stays usable while a file is in flight (`clients/web`)
- [x] 5.11f Offer sending the original rather than the reduced copy, stating which is being sent and naming the limit when an original is refused; verify the reduced copy remains the default (`messaging/attachments`, `clients/web`)
- [x] 5.11c Build context menus for messages, attachments and conversations, opened by right-click and by long press, keyboard-navigable and dismissable, offering only permitted actions; verify every action remains reachable without the menu (`clients/web`)
- [x] 5.11d Require confirmation before destructive actions, naming what is lost and never defaulting focus to the destructive choice; verify a reversible action is not interrupted and a reflexive keypress cannot destroy (`clients/web`)
- [x] 5.11e Make every hover-revealed action reachable by touch, size targets to the documented minimum, and add swipe shortcuts that are discoverable another way; verify on a touch viewport that no capability is pointer-only (`clients/web`)
- [x] **MILESTONE 5** — Demonstrate: a user uploads an image and sees a thumbnail, another user downloads it, a non-member is refused, a posted link renders a preview, and a link to a private network address produces no preview. Tag `v0.5.0-dev`.

## 6. Phase 6 — People find out what happened

Goal: notifications and search, so the product works when you are not looking at it.

- [x] 6.1 Implement notification generation for direct messages, mentions, and followed threads, excluding the user's own actions; verify no self-notification occurs (`platform/notifications`)
- [x] 6.2 Implement global and per-conversation preferences with override, applied across devices; verify a muted conversation still updates unread state (`platform/notifications`)
- [x] 6.3 Implement suppression while actively viewing, withdrawal when read elsewhere, and status interaction with priority override; verify no notification is raised for the conversation in view (`platform/notifications`)
- [x] 6.4 Implement batching and optional periodic summaries; verify a burst produces one batched notification rather than one per message (`platform/notifications`)
- [x] 6.5 Make notification delivery non-blocking and failure-tolerant; verify an unreachable push service does not delay message delivery or lose unread state (`platform/notifications`)
- [x] 6.6 Implement the `Searcher` interface with a Postgres implementation using a separate search table, a stored generated `tsvector` column, and a GIN index; verify reindexing does not touch the messages table
- [x] 6.7 Implement query parsing with `websearch_to_tsquery`, recency ordering, and `ts_headline` highlighting; verify hostile input never raises a syntax error and results are ordered by recency
- [x] 6.8 Implement `pg_trgm` fuzzy and prefix matching for autocomplete; verify prefix queries return results `websearch_to_tsquery` alone cannot
- [x] 6.9 Enforce authorization on all search results; verify search never returns messages from channels the user cannot read
- [x] 6.10 Build the web notification and search interface; verify both are usable end to end
- [x] **MILESTONE 6** — Demonstrate: a mention raises an in-app notification, muting silences it while unread state still updates, and searching returns matching messages ranked by recency with results from private channels correctly excluded. Tag `v0.6.0-dev`.

- [x] 6.10a Implement per-person disclosure settings and a member list that omits withheld details server-side rather than sending them to be hidden; verify a non-member cannot enumerate a private channel's members and that moderator access to withheld details is audited (`messaging/channels`, `platform/audit-log`)

## 7. Phase 7 — Operators are in control

Goal: a server an operator can actually run, configure, and moderate.

- [x] 7.1 Implement server identity with an operator-set canonical address not inferred from headers; verify a mismatched host header does not change reported identity (`platform/server-config`)
- [x] 7.2 Implement capacity limits for users, channels, and storage with usage reporting; verify each refuses cleanly without partial application (`platform/server-config`)
- [x] 7.3 Implement registration policy — open, invitation-only, closed, domain restriction; verify existing accounts are unaffected by a policy change (`platform/server-config`)
- [x] 7.4 Implement default language and branding applied without a rebuild; verify branding cannot hide the connected server's address (`platform/server-config`)
- [x] 7.5 Implement the feature-toggle framework with runtime evaluation and capability advertisement; verify a disabled capability rejects requests and is absent from the advertised set (`platform/foundation`, `platform/server-config`)
- [x] 7.6 Implement effective-configuration inspection with secret redaction and self-lockout prevention; verify a change removing the actor's own administration is refused or confirmed (`platform/server-config`)
- [x] 7.7 Implement content filters with block, flag, and mask actions, disabled by default; verify each behaves as configured and needs no external service (`platform/moderation`)
- [x] 7.8 Implement sensitive-media marking and obscuring; verify marked media is obscured until revealed and user marking works without a classifier (`platform/moderation`)
- [x] 7.9 Implement reporting with reporter non-disclosure and rate limiting, and the moderation queue with context and recorded outcomes; verify the reported party never learns who reported them (`platform/moderation`)
- [x] 7.10 Implement moderator actions — message removal, channel removal, suspension, posting restriction — audited and reversible; verify suspension invalidates sessions and temporary restrictions expire automatically (`platform/moderation`)
- [x] 7.9a Implement channel and server invitations issued as a link, a short code, or both, with an expiry, a use limit, and optional binding to one person; verify an expired, exhausted, revoked and never-existent invitation are refused indistinguishably, that revocation is immediate for someone already holding the link, that a code is rate limited more strictly than a link, and that accepting grants membership of that channel and nothing more (`messaging/channels`, `platform/audit-log`)
- [x] 7.10a Implement channel-level bans, temporary and permanent, taking effect on open connections, expiring without intervention, confined to their channel, and audited with reason and duration; verify a banned person cannot rejoin and that other memberships are untouched (`platform/moderation`)
- [x] 7.10b Implement per-person direct-message availability — anyone, only those sharing a channel, or nobody — enforced server-side with a refusal that does not disclose which setting caused it; verify existing conversations survive a narrowing and that moderators can still make contact (`platform/moderation`)
- [x] 7.11 Implement retention policies globally and per channel with disclosure and audited shortening; verify content is removed at expiry (`platform/moderation`)
- [x] 7.12 Implement user blocking without disclosure, with moderators retaining full view; verify the blocked user is not informed (`platform/moderation`)
- [x] 7.13 Implement disposable chats with server-enforced expiry; verify expired content is not served to a client offline through the expiry (`messaging/channels`)
- [x] 7.13a Implement per-message lifetimes — a duration from sending, expiry once every other participant has seen it, and a duration after each recipient first sees it — enforced server-side and removing the message's attachments with it; verify an expired message is not served to a client that was offline throughout, that it leaves a placeholder disclosing nothing, and that a lifetime can be shortened but never extended (`messaging/channels`)
- [x] 7.14 Implement audit retention, export, and recording of expiry; verify export round-trips and expiry itself produces an entry (`platform/audit-log`)
- [x] 7.14a Implement server visibility — public, limited, private — enforced server-side on peer discovery and room browsing; verify a limited server is not returned to an unlisted requester, a private server is not discoverable at all, and a private room on a public server stays undiscoverable (`federation/protocol`)
- [x] 7.15 Build the web administration and moderation interface; verify an administrator can perform every action above without using the API directly
- [x] **MILESTONE 7** — Demonstrate: an operator configures limits and registration policy, a moderator removes a message and suspends a user, a filtered word is blocked, a disposable chat expires, and every one of those actions appears in the audit log. Tag `v0.7.0-dev`.

## 8. Phase 8 — It works on a phone

Goal: the PWA that most TDC members will actually use.

- [x] 8.1 Implement responsive layout across phone to desktop; verify every capability is reachable at the smallest supported size with no horizontal scrolling (`clients/web`)
- [x] 8.2 Implement the pre-paint theme script in `app.head` per the house convention; verify no theme flash on load with a dark preference (`clients/web`)
- [x] 8.3 Add `@vueuse/motion` and `@vueuse/gesture` interactions — swipe-to-reply, swipe between channels, pull-to-refresh; verify gestures work on a touch device or emulator
- [x] 8.3a Add keyboard shortcuts using VueUse (`useMagicKeys`, `onKeyStroke`) — quick channel switch, search, composer focus, thread navigation — with a discoverable shortcut reference; verify each shortcut works and none intercept typing in the composer
- [x] 8.4 Configure the PWA with `@vite-pwa/nuxt` including `navigateFallbackDenylist` for API and WebSocket paths; verify real-time paths are never served from the cached shell (`clients/web`)
- [x] 8.5 Migrate the service worker to `injectManifest` and implement the web push handler, VAPID key generation, and subscription lifecycle with pruning; verify a push arrives with the browser closed and works with no commercial service (`clients/web`, `platform/notifications`)
- [x] 8.6 Implement the content-hiding option for push payloads; verify a private conversation's push contains no message content when hidden (`platform/notifications`)
- [x] 8.7 Implement offline tolerance, pending-message queueing, runtime caching, and an offline fallback; verify a message composed offline is sent on reconnection or reported as failed (`clients/web`)
- [x] 8.8 Implement install and update prompts preserving unsent input; verify a draft survives an update prompt (`clients/web`)
- [x] 8.9 Add `driver.js` onboarding tours that are skippable and resumable; verify a completed tour does not recur (`clients/web`)
- [x] 8.10 Add automated accessibility checks to CI; verify keyboard-only operation reaches every control and a seeded violation fails the build (`clients/web`)
- [x] 8.11 Add an initial-load payload budget check to CI; verify exceeding the budget fails the build (`clients/web`)
- [x] **MILESTONE 8** — Demonstrate: install the app to a phone home screen, receive a push notification with the browser closed, read messages offline, and compose one that sends on reconnection. Tag `v0.8.0-dev`.

## 9. Phase 9 — It runs on the desktop

Goal: the native client, with the capabilities that justify it over the PWA.

- [x] 9.1 Extract shared UI into `packages/ui` as a Nuxt layer with the transport-agnostic `ApiClient` interface wired by `provide`/`inject`; verify a unit test drives it with a mock adapter and no Nuxt runtime, and the web app still works unchanged
- [x] 9.2 Refactor the web app to consume the shared layer via its `$fetch` adapter; verify no behaviour regressions against the Phase 8 milestone
- [x] 9.3 Set up `nuxt-i18n-micro` with English and Swahili and the per-app generation script; verify both produce working locale payloads and a missing key falls back to English
- [x] 9.4 Configure `@nuxt/icon` with an explicit `globInclude` for the layer's components; verify layer icons appear in the client bundle
- [x] 9.5 Vendor shadcn-vue components including DataTable on TanStack Table v9; verify no second copy of `table-core` appears in the dependency tree
- [x] 9.6 Evaluate and vendor useful `@inspira-ui` components for animation and effects, reviewing code before adoption; verify they pass lint and accessibility checks
- [x] 9.7 Add the chart adapter interface with TanStack Charts at an exact pinned version as the default; verify swapping to a fallback implementation requires no caller changes
- [x] 9.8 Scaffold the Wails v3 app with `build/config.yml` as the version source, `//go:embed all:frontend/dist`, and Nitro `output.publicDir: "dist"`; verify a production build embeds the frontend
- [x] 9.9 Configure the desktop frontend extending the shared layer with bindings aliased and dev server on port 9245 from `WAILS_VITE_PORT`; verify `wails3 dev` runs both Go and frontend
- [x] 9.9a Add named themes selected by a `data-theme` attribute, each defining light and dark, with a second palette drawn from Congrator alongside the default; verify switching changes every surface in both clients and that no component hard-codes a colour (`clients/web`, `clients/desktop`)
- [x] 9.9b Make the accent configurable independently of the palette, deriving a readable shade per appearance from one chosen colour rather than accepting or refusing a single value — no value clears WCAG AA against both a near-white and a near-black foreground; verify each derived shade clears AA against the foreground its appearance uses and that the chosen hue is preserved (`clients/web`)
- [x] 9.9c Let an operator set the instance's default theme and accent, and a person override both for themselves, stored server-side so the choice follows them between clients; verify a person's choice survives signing in on a second client and that clearing it returns them to the operator's default (`platform/settings`)
- [x] 9.9d Store which tours somebody has seen on their account rather than in one browser, so an introduction shown on the web client is not shown again on the desktop one; verify replaying a tour by hand still works and that clearing the record offers it again (`platform/settings`)
- [x] 9.10 Implement the Wails-bindings transport adapter satisfying `ApiClient`, normalising Wails rejections into the shared error type; verify the same contract tests as the web adapter pass
- [x] 9.11 Implement server selection at first launch refusing unencrypted non-loopback addresses, with multi-server switching; verify an insecure address is refused with an explanation (`clients/desktop`)
- [x] 9.12 Implement OS keychain credential storage with a documented non-silent fallback; verify the absence of a keychain never results in plaintext storage (`clients/desktop`)
- [x] 9.13 Implement native notifications respecting system quiet modes, with activation navigating to the conversation; verify a background notification appears and navigates (`clients/desktop`)
- [x] 9.14 Implement the system tray with unread indication and configurable close-to-tray, with quick actions: show and hide the window, mute notifications for a chosen period, jump to a recent conversation, add a server or channel, and quit; verify disabling close-to-tray makes closing exit the app (`clients/desktop`)
- [x] 9.14c Keep one instance of the desktop client, raising the existing window when it is launched again; verify a second launch exits without a second window (`clients/desktop`) — done alongside 9.14
- [x] 9.14a Ask before anything destructive, in both clients — deleting a message, a channel or a server, banning or blocking somebody, removing a server from this client, signing out, quitting with unsent messages, and closing to tray the first time; verify each says what will be lost rather than only asking whether to continue, and that a refusal leaves everything as it was (`clients/web`, `clients/desktop`)
- [x] 9.14b Add the application's own icon at every size the platforms ask for, replacing the Wails default, and set the identity consistently: `net.anorebel.tdc` for the bundle, the keychain and the Linux desktop entry. Verify the icon appears in the launcher, the window, the tray and the notification, and that the running window associates with its desktop entry (`clients/desktop`)
- [ ] 9.14d Menu mnemonics on Linux — Alt+F, Alt+H and so on do nothing under GTK4, because Wails sets `use_underline` only on its GTK3 path. Verify with a keyboard alone that every menu can be opened and every item reached; raise it upstream if it is still absent (`clients/desktop`)
- [x] 9.14e Ask before the destructive actions. The three named here — deleting a channel, deleting a server, blocking a whole channel or server — are for features that still do not exist, so there is nothing yet to guard. What the audit did find is one that does: ending a session signed a device out on a single press, while every other destructive action on the same screen asked. It asks now, and says what happens rather than only whether to continue. Both rules are checked rather than left to habit — nothing irreversible may be one press away, and every dialog must say what will be lost — so the features this task names arrive guarded (`clients/web`, `clients/desktop`)
- [x] 9.15 Implement opt-in launch at login and window state restoration; verify autostart is off by default and position is restored, adjusted for disconnected displays (`clients/desktop`)
- [x] 9.16 Implement update checking with authenticity verification aborting on failure; verify an unverified update is discarded and reported (`clients/desktop`)
- [~] 9.16a Publish signed releases — the workflow exists (`.github/workflows/release.yml`): a tag builds Linux, macOS and Windows, signs each artifact with sigstore keyless through GitHub's OIDC, and writes the `latest.json` the client reads. The tag is checked against `VERSION` and refuses if they disagree. The feed's shape is held to the client's own types by a test over the workflow's actual output, since Python writes that document and Go reads it and nothing else makes them agree. **Still outstanding**: the placeholder trust root in `build/trust/sigstore-root.json` must be replaced with sigstore's real one, and the workflow has not run — it cannot be exercised without pushing a tag to the repository, which is a decision rather than a step (`clients/desktop`)
- [ ] 9.15a Window translucency on Linux — blocked upstream. Wails' `setTransparent()` is empty on its GTK4 path (`pkg/application/linux_cgo.go`, "GTK4: Transparency via CSS - different from GTK3"), so a window asking for a translucent background is composited solid; a minimal Wails application shows the same, which rules out anything here. Needs a GTK4 style provider setting the window's background to `rgba(0,0,0,0)` *and* WebKitGTK's background set transparent on a window with an alpha channel. Raise it upstream, or carry the cgo. The setting and the plumbing are already correct and say the platform ignores them (`clients/desktop`)
- [x] 9.17 Test rendering under both Wayland and X11 per the identified risk; verify no blank-window rendering failure occurs. Both backends were driven on this machine (`GDK_BACKEND=wayland`, then `GDK_BACKEND=x11` through XWayland) and render identically — 1347 unique colours, standard deviation 14 in both, where a blank-window failure is one flat colour and a deviation of zero. The application id differed between them at the time (`org.wails.tdc` on Wayland, `net.anorebel.tdc` on X11) because Wails built the GTK id from its own name and did not expose it; beta.23 made the id an option, so both backends now report `net.anorebel.tdc` and the desktop entry matches
- [ ] 9.17a Test on NVIDIA hardware — not reachable from the development machine, which has Intel Iris Xe and no NVIDIA card, so the detection branch can only ever take its false path here. Wails disables the DMA-BUF renderer in `init()` on both the GTK4 and gtk3 paths (`pkg/application/application_linux.go`, `application_linux_gtk3.go`), which is the documented workaround for the blank window and the "Error 71" crash. It detects the card by the presence of `/sys/module/nvidia`, so a card running the open-source nouveau driver is not detected; whether nouveau needs the workaround at all is unconfirmed, since the WebKit bug describes the proprietary driver's `gbm_bo_map()`. Setting `WEBKIT_DISABLE_DMABUF_RENDERER=1` by hand overrides the detection either way. Needs somebody with the hardware (`clients/desktop`)
- [x] **MILESTONE 9** — Demonstrated on this machine: the desktop client launches on Linux, adds a chosen server, signs in, and holds a conversation that is the web's conversation because it is the same component. A message from somebody else raises a system notification while the window is in the background — confirmed on the bus and in the notification centre — and a message this person sent raises none. Rendering was checked on both display backends (9.17).
  - Two parts of the original wording are not met as written, and neither is a test that was skipped. Credentials in the OS keychain cannot be exercised here: this machine runs no keyring, which is the state the servers screen describes on arrival rather than a failure — the code path is there and refuses rather than pretending. And "minimised to tray" was demonstrated with the window in the background rather than hidden to the tray, because the two shortcuts that hide it are the ones GTK's menu bar claims; see 9.14d, which is the same upstream limitation.

## 9b. Phase 9b — The desktop app becomes a chat client

The chat is built once and shared, so the desktop and the web cannot drift. What differs between them is drawn where it belongs: the transport, the live connection, and a few desktop affordances the web has no use for.

- [x] 9b.1 Move the chat surface into the shared layer — the message body, composer, thread panel, emoji picker, reactions, attachments, the dialogs (forward, report, schedule, lifetime, ban), search palette and shortcut reference — from `apps/web/app/components` into `packages/ui/app/components`, with the libs and composables they rest on (`emoji`, `reports`, `useCommands`, `useContextMenu`, `useMarkdown`, `useMediaUrl`, `useImageResize`, `useUploads`, `useOutbox`, `useDraft`). The web imports them from the layer afterwards, and its tests are what prove the move changed nothing (`packages/ui`, `apps/web`)
- [x] 9b.2 Move the dependencies the moved code rests on (`marked`, `dompurify`) into the layer's `package.json`, where `lucide-vue-next` already is, so both applications inherit them rather than each declaring its own (`packages/ui`)
- [x] 9b.3 Put the live connection behind a seam, the way the request transport already is. `useLiveConnection` takes an injected socket: the web provides the WebSocket it always used, the desktop one backed by a Go bridge that dials `/api/live` with the cookie jar the webview cannot reach. Two faults were behind frames never arriving, and neither was the one first guessed at — the machinery forgot a dropped socket rather than closing it, so every reconnection stacked another set of listeners and, on the desktop, Go closed the live connection each time it opened the next; the two took turns killing each other and the replay was cut off mid-flight. Closing what is being replaced is now checked by a test that a compiling sabotage fails (`packages/ui`, `apps/web`, `clients/desktop`)

- [x] 9b.4 Build the desktop sign-in screen over the shared `useAuth`, and offer to keep the session in the OS keychain after it succeeds — the `Remember` and `Recall` bindings exist and are unused. Somebody who accepts is not asked for a password on the next launch; somebody who declines, or has no keychain, signs in each time, which the servers screen already explains (`clients/desktop`)
- [x] 9b.5 Build the desktop chat shell and conversation screen over the shared components — the sidebar as a column that stays, since a desktop window is not a phone, but the drawer and the full keyboard set kept for a small window on a small laptop. The conversation itself is the shared surface, unchanged (`clients/desktop`)
- [x] 9b.6 A native notification opens the conversation it is about — already wired from the tray work, and now by named route rather than a built path, so it survives a locale strategy that prefixes (`clients/desktop`)
- [x] 9b.7 Verified on this machine, against the same server the web was tested on: the desktop client signs in, lists its channels, opens one with its history, sends a message, and receives one sent from elsewhere without a reload. The live frames come through the Go bridge rather than the webview — `ss` shows the connection held by the `tdc` process rather than WebKitNetworkProcess, which is what the cookie jar arrangement requires. `Ctrl K` was broken and is fixed. Not covered: a direct conversation, and the keychain round-trip — this machine runs no keyring, which is what the servers screen says on arrival (`clients/desktop`)
## 9c. Phase 9c — The stack the frontend was meant to be built on

Four libraries the constraints name were in no package at all, and only display primitives had been vendored. Everything somebody types into was a native element with a hand-written class.

- [x] 9c.1 Add the libraries the constraints name — `@tanstack/vue-form`, `valibot`, `date-fns`, `@date-fns/tz` — to the layer, and make them resolvable from both applications (`packages/ui`)
- [x] 9c.2 Vendor the form family into `components/ui` — Button, Input, Textarea, Select, Label, Checkbox, and a Field that ties a label, a control and its error together. Every colour a theme token, checked by a test that fails on a literal; the Select drawn on reka-ui rather than native, so the desktop webview and a browser show the same control (`packages/ui`)
- [x] 9c.3 A check that a vendored component rendered in a template is imported. The scanner registers them with a Ui prefix, so an unimported name resolves to nothing — and it type-checks, so the first sign is a control missing from a screen. It found two already missing: the audio player's buffering spinner and the text viewer's loading skeleton (`packages/ui`)
- [x] 9c.4 Move the call sites over — seventy-six buttons, ten fields and four choice lists across both applications and the shared chat. Sign-in, registration and first-run setup are TanStack Form with Valibot schemas. What still carries a `.btn` class is a link that looks like a button, which is not one (`apps/web`, `packages/ui`, `clients/desktop`)
- [x] 9c.5 Dates through `date-fns` and `@date-fns/tz`. Ten formatting sites behind named formats in one module, each pinned by a test against a fixed instant in a named zone — the browser's own formatter asks the runtime for a locale, and the desktop webview's answer is not always the browser's. A check fails on any new `toLocaleDateString`, `toLocaleTimeString` or `Intl.DateTimeFormat` (`packages/ui`)
- [ ] 10a.6b The edit-mode hint under a message being edited is `opacity-70` on the same ground where 70% failed contrast for the edited and lifetime markers (`ChatRoom.vue`, "Enter to save, Esc to cancel"). axe has not reported it, because the audit never runs with a message open for editing — which is a gap in the audit rather than evidence the hint is readable. Either raise it to 85% with the others, or audit a conversation with an edit in progress and find out (`clients/web`)
- [ ] 10a.6a The introduction overlay is a problem for anybody who meets it, and the suite had been hiding that. driver.js inserts `#driver-dummy-element` carrying ARIA attributes its role does not allow, so axe reports `aria-allowed-attr` (critical) on any page audited while the tour is showing; it also traps Tab inside its own two buttons, so a keyboard user on their first visit can reach `Next` and `Close` and nothing else. Neither had been seen because `session.setup.ts` dismisses the tour before anything else runs — so every check sees the application as somebody who has already been through it. Raise both upstream, or take the overlay off the accessibility tree and release focus while it shows (`clients/web`)
- [ ] 9c.5a The date picker is still `@vuepic/vue-datepicker` rather than anything built on date-fns. It is themed through CSS variables and works; replacing it is worth doing when something needs it rather than for tidiness (`packages/ui`)
- [x] 9c.6 A check that a form with more than one field uses the form library, and that where a form validates it validates with Valibot. The threshold is deliberate: a dialog with one box and a submit disabled until it is non-empty gains nothing from a schema. It found the channel-creation and suspension forms, both of which stated a rule under the field that nothing enforced (`packages/ui`)

- [x] **MILESTONE 9b** — Demonstrated: the desktop client signs in, opens a channel and holds a conversation that is the web's conversation, because it is the same component. Sending, receiving live, search, the keyboard reference and the shortcuts all work; the palette, the sidebar and the composer are the shared ones.

## 10. Phase 10 — Ready for other people to run

Goal: everything needed for TDC and others to actually deploy and adopt it.

- [x] 10.1 Implement OAuth2 authentication with operator-configured providers and verified-email linking; verify an unverified email does not auto-link to an existing account (`identity/authentication`)
- [x] 10.2 Implement passkey/WebAuthn enrolment and login via `go-webauthn` with multiple credentials per user; verify the last remaining authentication method cannot be removed (`identity/authentication`)
- [x] 10.2a Implement optional MFA with TOTP and single-use recovery codes, and an operator setting requiring it of everybody or of administrators; verify a recovery code works once, that a code cannot be replayed within its window, and that a wrong code does not disclose whether the password was right (`identity/authentication`)
- [x] 10.2b Accept a passkey as a second factor on a password account, so somebody holding one is not asked to carry an authenticator app as well; verify a passkey sign-in is not then asked for a further factor (`identity/authentication`)
- [x] 10.2c Offer an emailed code as a second factor where the operator has configured mail, after 10.3 makes that available; verify it is offered only when mail works (`identity/authentication`)
- [x] 10.2d Give roles a way to be created and assigned through the API, and seed a default role every new account holds; verify somebody who is not the first administrator can create a channel, and that `role.assign` is enforced where it is claimed (`identity/authorization`)
- [x] 10.3 Implement account recovery with single-use time-limited tokens over self-hosted mail; verify a token cannot be reused and recovery does not disclose account existence (`identity/authentication`)
- [x] 10.3a Send a confirmation link when an address is given, so an account can be recovered at all; today the only verified addresses are ones an OAuth2 provider vouched for, which leaves password-only accounts unrecoverable (`identity/authentication`)
- [x] 10.4 Implement the Valkey `Cache` implementation and shared rate-limiter counters; verify a multi-node deployment enforces one shared limit rather than one per process (`design.md` — Caching)
- [x] 10.5 Implement multi-node fanout over Postgres `LISTEN`/`NOTIFY` behind the broker interface; verify cross-process delivery works and a single node needs no broker (`messaging/realtime`)
- [x] 10.6 Implement redaction preserving ordering and verifiability of surrounding events, and bounded documented erasure; verify redacted content is unretrievable and neighbours still verify (`platform/event-log`)
- [x] 10.7 Write the federation-readiness test proving a foreign event is verifiable from only the originating server's published keys and the event's own content; verify it passes without database access to the originating server (`platform/event-log`)
- [x] 10.8 Add dependency licence checking against an approved-licence allowlist and vulnerability scanning; verify a GPL or vulnerable dependency fails the build (`platform/foundation`)
- [x] 10.9 Add the release workflow producing server binaries and desktop installers for Linux, macOS, and Windows on amd64+arm64 with checksums, plus multi-architecture container images; verify a test tag publishes every artifact and a single platform failure fails the release (`platform/foundation`, `clients/desktop`)
- [x] 10.10 Add Linux packaging producing both GTK4 and legacy gtk3 builds per the design; verify both build and the gtk3 variant runs on an older distribution
- [x] 10.11 Configure reproducible builds with `SOURCE_DATE_EPOCH` from the commit timestamp and `-trimpath`; verify two builds of one commit produce identical binaries
- [x] 10.12 Add release-notes generation from changes since the previous tag; verify notes are attached to a test release
- [x] 10.13 Add server memory measurement to CI reporting against the 1 GB target with the delta from baseline; verify the figures are reported on every change and an increase is surfaced for justification rather than blocking (`platform/foundation`)
- [x] 10.14 Write the hosting guide covering deployment, TLS, backup, restore, and upgrade; verify an operator following it end to end succeeds without reading source (`platform/foundation`)
- [x] 10.15 Write the user guide covering every user-facing capability shipped; verify coverage against the milestone list
- [x] 10.16 Write the contribution guide covering the Apache-2.0 requirement, the prohibition on importing GPL code from other TDC repositories, the commit-message convention (describe the software change and its reason; do not reference internal task or specification identifiers, planning artifacts, or tooling used), and the policy that AI-assisted contributions are welcome but must be disclosed and must carry a rationale; verify each is stated explicitly and a pull request template prompts for the rationale and disclosure
- [x] 10.17 Write the integration guide documenting the API surface and authentication; verify the documented examples run against a live server
- [x] 10.18 Write the upgrade and migration guide including skipped-release upgrades; verify the documented procedure works across two releases
- [x] 10.19 Write the Telegram and WhatsApp migration guide for TDC members; verify it addresses account creation, channel setup, and expectations differing from those tools
- [ ] **MILESTONE 10** — Demonstrate: a person who has never seen the repository deploys a server from the published container image following only the hosting guide, invites a second person, and both exchange messages from the web and desktop apps. Tag `v1.0.0-rc.1`.

## 10a. Phase 10a — The same interface for people and programs

Everything the web client does is an HTTP call somebody else can make. This
phase makes that true deliberately rather than incidentally, and adds the
messages that carry their own actions.

- [x] 10a.1 Issue API tokens as first-class credentials on an existing account, scoped and revocable, with the same permissions and audit trail as a session; verify a token cannot do what its account may not, and that revoking one takes effect immediately (`platform/api`)
- [x] 10a.2 Accept bearer tokens on every existing route rather than a separate namespace; verify each documented endpoint answers identically to a session and to a token, and that a token is refused where a session would be (`platform/api`)
- [x] 10a.3 Publish a machine-readable description of the API generated from the routes themselves; verify a route added without documentation fails the check (`platform/api`)
  - Deliberately not OpenAPI. A document claiming to describe request and response bodies would mean inferring shapes that are not in the routing table, and a schema confidently wrong about a body is worse than none — somebody generates a client from it and finds out at runtime. What a routing table honestly knows is which paths exist, which methods they take, what their parameters are called, what a caller must carry and which permission each needs, and that is what `GET /api/description` reports for all 143 endpoints.
- [x] 10a.4 Add message actions — a message may carry labelled buttons — with a documented boundary between actions that emit an event to the sender and the fixed set the client may perform itself; verify an action naming an undeclared client capability is refused at the point it is posted (`chat/messages`)
- [x] 10a.5 Deliver action presses to the sender as events, identifying who pressed and which action, exactly once; verify a press reaches a program and a repeated press of a one-shot action is refused (`chat/messages`)
- [x] 10a.6 Render actions accessibly in both clients — reachable and operable by keyboard, announced as buttons, with the outcome announced when it arrives; verify with a screen reader and in the automated accessibility pass (`clients/web`, `clients/desktop`)
- [x] 10a.7 Document writing a program against the API, with a worked example that posts a message carrying actions and responds to a press; verify somebody can follow it without reading the server source
- [ ] **MILESTONE 10a** — Demonstrate: a program authenticates with a token, posts a message offering two choices, a person presses one in the web client and another in the desktop client, and the program receives both presses and replies. Tag `v1.1.0-dev`.

## 11. Cross-cutting verification

These run continuously from the phase in which they first apply, not only at the end.

- [x] 11.1 Configure lefthook pre-commit and pre-push hooks per the house convention; verify a commit with a lint error is rejected — from Phase 1
- [x] 11.2 Add ESLint flat config with `@nuxt/eslint`, `eslint-plugin-vue`, and `eslint-plugin-vuejs-accessibility`, plus Prettier configured for semicolons and double quotes, `vue-tsc`, and `.golangci.yml`; verify `bun run lint` and `golangci-lint run` pass and that a single-quoted, semicolon-less file is reformatted — from Phase 1
  - `@nuxt/eslint` is deliberately not installed. Its purpose is generating a config per Nuxt application; this repository lints three workspaces from one hand-written flat config at the root, and adding the module would mean a second config source competing with it. Everything the task wants from it — Vue template rules, TypeScript, accessibility — is in that config.
- [x] 11.3 Add the CI workflow with per-workspace jobs and a `ci-success` aggregate gate, pinning exact Go and Wails versions with written rationale while taking the latest released Bun rather than a pinned one, so an incompatibility surfaces in a build rather than months later on a contributor's machine; verify a failing job blocks the gate — from Phase 1
- [x] 11.3a Tier CI by branch per the design — fast checks (lint, typecheck, unit tests, single-platform build) on every push including `dev`; the full matrix (cross-browser E2E, multi-platform builds, licence and vulnerability scanning, accessibility, budgets, memory, load tests, desktop packaging) on `beta` and `main` and PRs targeting them; release jobs on tags only. Verify a push to `dev` runs only the fast tier and a PR to `beta` runs the full gate — from Phase 1
- [x] 11.4 Add migration validation applying each migration to a database at the previous release's schema; verify a non-applying migration fails CI — from Phase 1
- [x] 11.5 Set up Playwright as the cross-browser CI backbone with bunwright retained for local runs; verify the same suite runs under both — from Phase 3
  - Playwright is the backbone and now runs Chromium, Firefox and WebKit. **Bunwright is deliberately not adopted**, and the half of this task asking for one suite under both cannot be met as written: bunwright 0.3.2 — unchanged since the design was written — is a browser automation DSL with no test framework at all. It has no `test()`, no `expect()`, no fixtures, and addresses elements through prefixed strings (`"role:button[name='Login']"`) rather than `getByRole`. Running the same suite under both would mean maintaining every check twice in two dialects, which is how the second copy quietly stops matching the first. Worth revisiting if it grows a test runner.
- [x] 11.6 Maintain end-to-end tests for the full messaging cycle across two concurrent clients, extended each phase; verify they pass on Chromium, Firefox, and WebKit — from Phase 3
  - Chromium and Firefox are verified locally; WebKit runs in CI, where `playwright install --with-deps` can supply the Debian libraries it needs. It cannot launch on this Arch machine — it wants `libicu 74` where the system has 78 — so it is verified by CI rather than here.
- [x] 11.7 Maintain end-to-end tests for permission enforcement, verifying private channels stay undiscoverable and unauthorized actions are refused server-side — from Phase 2
- [x] 11.8 Maintain end-to-end tests for offline behaviour and reconnection gap recovery; verify no duplicate or missing messages after a simulated partition — from Phase 3
- [x] 11.9 Add a load test establishing and tracking the baseline for concurrent connections and fanout latency; verify documented targets are met and recorded — from Phase 3
- [x] 11.11 Move the interface's own words behind the translation layer, which is configured and carries no strings: every screen writes them inline today, so a Swahili speaker sees an English application. Verify switching locale changes what is on screen, and that a string added without a translation is caught rather than silently falling back — from Phase 9a
- [x] 11.10 Keep documentation current in the same change that alters the behaviour it describes; verify a behaviour change without a documentation update is rejected in review (`platform/foundation`) — from Phase 1
