## Why

A status says somebody is away to whoever looks for it. Most people who need an answer do not look: they send a direct message, or name the person in a channel, and wait. Somebody on leave for a week has no way to tell them so, short of answering every message by hand — which is the thing being away means not doing.

## What Changes

- **Automatic replies, kept per person.** Each has its text (Markdown, at most 500 characters), optionally different words for mentions, and is tied to statuses, a window of time chosen in a time zone, both, or neither ("until I change it"). Several may be kept; the first made that applies answers.
- **Posted by the server, as the person, marked.** A direct message is answered in the conversation; a mention in a channel is answered in the thread of the mentioning message. The reply carries `via.kind = "automatic"` in its own content, as a program's message carries its `via`, so every client, export and delivery can tell it from the person typing.
- **Anti-noise rules.** Never in answer to a program, a webhook, another automatic reply or the person themselves; once per sender per conversation per day; at most twenty conversations an hour per person; never on an edit.
- **Privacy rules.** Nobody either side has blocked, nor anybody the person's direct-message setting would not admit, is answered. A reply tied to a status stays silent while the person appears offline. Nothing about a reply depends on whether the person is connected. Conversations another server holds are not answered (local only).
- **Status coupling.** The status dialog offers "Turn on my automatic reply" for the status being chosen; that reply is tied to the status and stops when it changes or clears. The foot of the sidebar says when a reply is on, and `GET /api/me/status` carries `automaticReply`.
- **Settings.** A section to create, edit, switch on and off, delete and preview replies, in both clients through the shared layer, loaded only when settings is opened.

**Out of scope:** answering people on other servers; a new webhook event type (an automatic reply is an ordinary `message.sent` with its mark).

## Capabilities

### Modified Capabilities

- `messaging/realtime`: a new requirement, Automatic replies, beside Status.

## Impact

- **API:** `GET/POST /api/me/automatic-replies`, `PUT/DELETE /api/me/automatic-replies/:id`; `automaticReply` on `GET/PUT /api/me/status`; a new `via` kind, `automatic`.
- **Schema:** migration 000432 — `automatic_replies` and `automatic_reply_log`, both cascading from the account, so erasure takes them.
- **Integrations:** bots receive automatic replies as messages carrying `via.kind = "automatic"`, and are never answered by one.
