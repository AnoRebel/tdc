Scope note: these tasks implement the Automatic replies requirement of `messaging/realtime` against the running platform. Tasks reference the spec for what to build and `design.md` for how.

**Documentation rule: documentation is written as the work is done.** The user guide's automatic replies section and the integration guide's endpoints stay current with each task.

## 1. Server

- [x] 1.1 Migration 000432: `automatic_replies` and `automatic_reply_log`, cascading from the account; sqlc queries for listing, creating, changing, deleting, counting the last hour, and claiming and releasing a reply
- [x] 1.2 `autoreply` package: validation (text 1–500, window order, zone, at least one kind), `Applies` against the person's own status with appearing offline treated as `offline`, `Choose` first-made-wins, and the 24-hour claim with the hourly ceiling of 20
- [x] 1.3 `GET/POST /api/me/automatic-replies`, `PUT/DELETE /api/me/automatic-replies/:id`, statuses checked against the operator's; `automaticReply` on the own status; catalogued, OpenAPI record regenerated
- [x] 1.4 Firing after a message and after a thread reply: people only (no `via`, a sender account), direct conversations and mentions, threads for mentions, blocks either way and the direct-message setting, local rooms only; posted as the person with `via.kind = "automatic"`, notified and published
- [x] 1.5 Tests against a database: answers a direct message once; answers a mention in its thread with the mention text; silent outside its window and status; silent while appearing offline for a status-tied reply and answering for one tied to appearing offline; never a bot, oneself or another automatic reply; never across a block in either direction or a closed direct-message setting; validation. Each guard broken in turn and the test seen to fail

## 2. Clients

- [x] 2.1 Types: `AutomaticReply`, `automaticReply` on `OwnStatus`, `automatic` as a `via` kind
- [x] 2.2 Settings section (TanStack Form and Valibot): list with on/off, edit, delete with confirmation, editor with statuses, window in a chosen zone, kinds answered, mention text, and a live preview; loaded lazily
- [x] 2.3 Status dialog: "Turn on my automatic reply" with its text for any status but available, kept as the reply tied to that status alone
- [x] 2.4 The foot of the sidebar says when a reply is on; conversations and threads mark automatic replies in words and keep them apart from the person's own messages
- [x] 2.5 English and Swahili; payload budget unchanged; verified in a browser with two people

## 3. Documentation

- [x] 3.1 User guide: Automatic replies, beside presence and status
- [x] 3.2 Integration guide: the endpoints and what an integration should make of `via.kind = "automatic"`; webhooks and applications: how bots see them
