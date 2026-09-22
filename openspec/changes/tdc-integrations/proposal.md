## Why

TDC's foundation change built the primitives that let a program act on the platform — API tokens scoped and intersected against the account's live permissions, and messages that carry buttons whose presses arrive as ordinary events over the live connection. What it deliberately left out is everything that lets an external system participate *without* holding a session: posting into a channel from outside, being told when something happens without polling, and offering more than a row of buttons.

That gap matters for TDC specifically. The community's actual workflows — CI results, deployment approvals, issue-tracker mirroring, a build bot, a moderation bridge — all live on the other side of an HTTP boundary from the server. Without inbound and outbound webhooks, every one of those needs a person to copy results into chat by hand, which is exactly the friction TDC is trying to leave behind by moving off Telegram and WhatsApp. And without applications as a first-class concept — installed, scoped, revocable, visible to the people they can see — the only way to give an external system standing access today is to hand it a personal API token, which ties its lifetime and its permissions to one person's account rather than to the integration itself.

This change closes that gap: inbound and outbound webhooks, third-party applications as installable, scoped, revocable entities distinct from the tokens that already exist, bots as a visible participant kind, and the rest of interactive messages — selection menus, input fields, updating a message in place, private responses, and the signed delivery and per-integration rate limiting those all depend on.

## What Changes

**Already built, in a prior change, and not rebuilt here:**

- **API tokens** (`server/internal/apitokens/`) — first-class credentials on an account, scoped to a subset of permissions, intersected with the account's live permissions on every request rather than frozen at issue, revocable with immediate effect, shown once. This is the credential primitive; applications in this change are issued *their own* tokens through the same mechanism rather than a parallel one.
- **The message action schema and validation** (`server/internal/actions/`) — buttons with a name, a label, `once`, `expires`, and the fixed client-capability boundary (`open`, today), validated at post time so a malformed action never reaches a channel.
- **Presses as events, exactly once** (`server/internal/rooms/presses.go`) — a press is appended to the room's event log and delivered over the same live connection as everything else, with single-use enforced by a unique constraint rather than application logic, and a stale or already-answered press refused before anything is recorded.
- **The generated API description** (`GET /api/description`, `server/internal/httpapi/description.go`) — every route self-documents from the routing table; a new route this change adds without an entry fails the existing check.
- **A worked integration example** (`docs/integrating.md`) — authenticate with a token, post a message with actions, listen on the live connection, respond to a press. This change extends that document; it does not start it.

**Built here:**

- **Outgoing webhooks** — a user subscribes an external endpoint to event types they can see; the server delivers signed, retried, backed-off HTTP requests; a persistently failing subscription is disabled rather than retried forever; delivery history is visible to the subscription's owner.
- **Incoming webhooks** — a channel-bound, revocable credential an external system posts a message with; scoped to exactly that channel; attributed to the webhook, not to a person.
- **Delivery signing and verification** — every outgoing delivery (webhook events and, sharing the same mechanism, interactive-message invocations) is signed so a receiver can prove it came from this server and was not altered, with a timestamp that bounds replay.
- **Outbound request safety for webhook delivery** — the same connect-time SSRF defence already built for link unfurling (`server/internal/unfurl/`), applied to webhook delivery so a subscription cannot be used to reach the server's own internal network.
- **Rate limiting for both directions**, sharing the same limiter used for authentication (`server/internal/cache/`), with limit-exceeded made visible rather than silently dropping events.
- **Applications** — registration, explicit and informed approval, scoped and channel-restricted access, their own revocable credentials issued through the existing token mechanism, visibility to the people they can see, and removal with a choice about the content they created.
- **Bots** — an application acting as a distinguishable participant: its own identity in a room, subject to the same permissions, rate limits, and moderation as anyone else.
- **The rest of interactive messages** — selection menus and short input fields alongside buttons; responding by updating the originating message atomically; a private response visible only to the invoker; signed delivery of an invocation to the integration that posted the message (as opposed to a press, which already stays inside the server as an event); per-integration rate limiting distinct from per-user; and programmatic multi-step exchanges that carry context between steps without leaking it to other users.

## Capabilities

### New Capabilities

- `integrations/webhooks`: incoming and outgoing webhooks, delivery signing, retry and backoff, outbound request safety, rate limiting, payload versioning.
- `integrations/apps`: application registration and approval, scoped and channel-restricted access, application credentials (via the existing token mechanism), bots, rate limiting and isolation, visibility, removal.
- `integrations/interactive-messages`: selection menus and input fields, responding by update/new-message/private-reply, signed invocation delivery, per-integration rate limiting, programmatic multi-step conversation. (Buttons, presses, one-shot/expiry enforcement, and accessible rendering are already built and are extended rather than replaced.)

### Modified Capabilities

None. This is additive to the foundation; no previously shipped behaviour changes.

## Impact

- **Server**: new packages for webhook delivery (queue, retry, signing, history) and applications/bots (registration, approval, credential issuance riding on `apitokens`), new tables for webhooks, deliveries, applications, and bot participants, new outbound HTTP traffic the operator's network must permit.
- **Operators**: a new thing to watch — delivery health per subscription — and a new decision — whether to permit outbound requests to private ranges for a trusted internal receiver, which the spec requires to be off by default.
- **Zero-cost self-hosting**: delivery, signing, and retry require no third-party service; nothing here needs a paid dependency. An operator runs a server-owned outbound queue against Postgres, the same datastore as everything else.
- **Security surface**: an incoming webhook's credential is a secret pasted into another service by design, and this change's design.md treats leakage as the expected case — scoped to one channel, individually revocable, and its use audited — rather than adding it as an afterthought.
- **Documentation**: `docs/integrating.md` gains sections for registering an application, creating incoming and outgoing webhooks, verifying a delivery's signature, and the interactive-message elements beyond buttons — written alongside the code that makes each true, per house convention.
