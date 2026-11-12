## Why

Files are the weakest part of a conversation today. An upload is one request, so a large file needs a proxy that accepts a body that large, holds a connection open for minutes, and starts again from nothing when a train goes into a tunnel. The limits are compiled in (25 MiB a file), so an operator cannot raise or lower them without a rebuild, and nothing stops one person filling the disk. A message carrying only files is refused with "message is empty" on the server, though both clients offer to send one. Files sent into a room shared with another server never reach the people there: the event carries the text and nothing else. The desktop client reads a file whole into memory and cannot save anything over 64 MiB. And the spec already allows a direct transfer between two devices — for a file nobody wants resting on a server — but nothing provides one.

## What Changes

- **A message may be nothing but its files**, everywhere: channels, threads, direct and group conversations, scheduled messages and rooms another server holds. Still refused when there is neither text nor a file.
- **Resumable uploads in pieces.** `POST /api/rooms/:id/uploads` declares a file; pieces are sent in order (`PUT /api/uploads/:id/pieces/:n`, raw bytes or base64 JSON), each stored as its own object so any node can take any piece; `POST /api/uploads/:id/finish` runs the same checks a single upload does and returns the attachment. `GET` says how far it got, so a client resumes rather than restarts; `DELETE` abandons it. Both clients use it for every file, with real progress on the desktop too. The single-request upload stays for integrations.
- **Limits and quotas become runtime settings**: largest file, largest message, a per-person quota, the piece size (for proxies with small body limits), and how long an unfinished upload or an unsent file is kept. The server-wide storage limit now counts uploads in progress, so it cannot be overrun by many at once.
- **Cleanup that actually runs.** Abandoned uploads and files that never became part of a message are removed on a schedule; neither was before.
- **Files travel with federated messages.** A message's files are described in its signed content (name, type, size, SHA-256). Another server records them and fetches the bytes on first view, with a signed request to the room's home server, checks the digest, and keeps a copy — up to a size the operator chooses. The home server fetches from the author's server the same way. Nothing new is trusted: the digest is signed by the author's server.
- **Direct transfers.** In a direct conversation, a person may offer a file straight to the other person's device over a WebRTC data channel, negotiated through the server's existing authorised live connection with the call ICE configuration. The recipient accepts or declines; nothing is sent before they accept; the file never touches the server. If either person is offline, or the connection cannot be made, the sender is offered an ordinary upload instead.
- **Desktop downloads stream to disk** through a save dialog, so a large file no longer has to fit in memory.
- **Documentation**: sending and receiving files and direct transfers for members; settings, a disk and bandwidth guide with worked numbers, proxy body sizes, quotas, relay cost, federation storage, cleanup and backup for operators; the upload protocol for integrators.

**Out of scope:** direct transfers to somebody on another server (signalling does not cross servers); resuming a direct transfer part way (a failed one falls back to an upload); end-to-end encryption of stored files; per-room quotas.

## Capabilities

### Modified Capabilities

- `messaging/attachments`: attaching files (empty text allowed, resumable uploads, quotas as settings), direct transfers, deletion and cleanup.
- `federation/protocol`: message propagation carries files, and how a peer fetches one.

## Impact

- **API:** `POST /api/rooms/:id/uploads`, `GET|DELETE /api/uploads/:id`, `PUT /api/uploads/:id/pieces/:piece`, `POST /api/uploads/:id/finish`; `attachmentIds` on `POST /api/rooms/:id/remote/messages`; `GET /api/attachments/limits` gains the quota, what is used and the piece size; direct transfers under `/api/rooms/:id/transfers` and `/api/transfers/:id`; `GET /api/transfers/ice`; federation `GET /api/federation/rooms/:slug/files/:digest`.
- **Events:** `message.sent` content gains `attachments` (descriptors) when a message carries files — additive; webhooks see it.
- **Realtime:** `transfer.offered`, `transfer.updated`, `transfer.signal` frames; a `transfer` client action.
- **Schema:** migration 000436 — `file_uploads` (excluded from backups: in-progress and short-lived), `attachments.origin_server`/`fetched_at` (remote files), `direct_transfers` (excluded: ephemeral negotiation).
- **Settings:** `files.max_file_bytes`, `files.max_message_bytes`, `files.quota_per_person_bytes`, `files.upload_piece_bytes`, `files.abandoned_after_hours`, `files.federation_copy_max_bytes`, `files.direct_transfer_relay`; feature `direct-transfers`.
