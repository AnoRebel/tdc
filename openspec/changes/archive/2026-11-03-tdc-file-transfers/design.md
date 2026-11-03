## Context

An attachment is uploaded before the message that carries it (`POST /api/rooms/:id/attachments`, multipart), spooled to disk, measured, typed from its bytes, scanned if a scanner is configured, stored under its SHA-256 in the configured store (local directory or S3), given a preview if it is an image, and recorded in `attachments` with no event. Sending the message binds the upload to the new event (`AttachToEvent`, scoped to the uploader and the room). Downloads check room membership on every request and serve inertly (nosniff, sandboxing CSP, attachment disposition for anything not plainly media); an object store gets a 30-second presigned redirect issued after the check. Limits are `attachments.DefaultLimits()`, compiled in. `limits.max_storage_bytes` is the only runtime limit. `ReclaimOrphans` exists and is never called.

Backups already upload archives in pieces: numbered, each stored as its own object so any node behind a load balancer can take any piece, joined on finish. Calls negotiate over the authorised live connection (`signal` action, re-authorised on every frame) and fetch ICE servers from `/api/calls/ice`, with TURN credentials minted per person.

Federated events are signed by the author's server over a digest of their content; the home server sequences without re-signing. Message content is `{"body": ...}` and nothing about attachments crosses.

## Decisions

### Text is optional when there are files

`rooms.SendMessage` and `ReplyInThread` refuse an empty body; they gain variants that take the message's files, and refuse only when there is neither text nor a file. The same holds for scheduled messages (which carry attachment ids already), for posting into a room another server holds, and for a remote post arriving at the home server (whose content then carries descriptors). Editing still requires text: an edit replaces the body, and an empty one is a deletion by another name.

### Uploads in pieces, on the backup pattern

Own protocol rather than tus. tus is a good protocol, but the parts of it that matter here are four requests, the server side would be a dependency doing what thirty lines do, and the backup upload has already settled the house shape:

- `POST /api/rooms/:id/uploads {filename, size}` — membership, the per-file limit, the person's quota and the server's limit are all checked **here**, against the declared size, before a byte is sent. Answers `{id, size, received, pieces, pieceBytes, expiresAt}`.
- `PUT /api/uploads/:id/pieces/:n` — body is the piece's raw bytes (`application/octet-stream`) or `{"data": base64}` JSON. Raw is what the web sends (no 33 % overhead); JSON is what the desktop sends, because its transport carries JSON through Go. Only the next piece is accepted, so a retried piece is harmless; the conditional update that advances the count is what decides, and a piece that loses the race is deleted. Every piece but the last must be exactly `pieceBytes`; the total may never pass the declared size.
- `GET /api/uploads/:id` — how many pieces the server holds, so a client resumes from there.
- `POST /api/uploads/:id/finish` — joins the pieces into a spool file and runs exactly the single-upload path (type detection, type limits, scan, hash, store, preview, record). The pieces are deleted afterwards whatever happens.
- `DELETE /api/uploads/:id` — abandons it.

Only the uploader may touch an upload; anybody else gets 404. Pieces live in the store under `uploads/<id>/<n>`, never under a content key, so nothing can reach them as an attachment and backups (which follow attachment rows) never see them.

`files.upload_piece_bytes` (default 4 MiB, 256 KiB–8 MiB) exists for proxies: the default nginx `client_max_body_size` is 1 MiB, and an operator who cannot change the proxy can make the pieces fit it. The single-request route still needs a body as large as the largest file, so it is documented as the integrations' route.

Memory per piece is bounded: a piece is read through `MaxBytesReader` into the store, never more than `pieceBytes` (plus base64) in flight.

### Limits and quotas as settings

| Setting | Default | Meaning |
|---|---|---|
| `files.max_file_bytes` | 100 MiB | Largest single file |
| `files.max_message_bytes` | 250 MiB | Largest total across one message |
| `files.quota_per_person_bytes` | 0 (none) | What one person's files may occupy, counted per distinct stored file they uploaded, plus their uploads in progress |
| `limits.max_storage_bytes` | 0 (none) | Existing server-wide limit; now counts uploads in progress at their declared size, and copies of other servers' files |
| `files.upload_piece_bytes` | 4 MiB | Piece size clients are told to use |
| `files.abandoned_after_hours` | 24 | An unfinished upload, or an upload never sent in a message, is removed after this |
| `files.federation_copy_max_bytes` | 100 MiB | Largest file this server copies from another; 0 is no limit |
| `files.direct_transfer_relay` | true | Whether direct transfers may use the TURN relay |

Files per message stays at 10 and the blocked executable types stay as they are. The attachments service reads limits through a function the HTTP layer supplies from settings, so a change applies to the next request without a restart. The 25 → 100 MiB default is deliberate now that a large file no longer needs one long request; an operator on a small disk lowers it.

Reserving the declared size at the start, rather than counting as pieces arrive, is what makes the limits limits: twenty people each starting a 100 MiB upload against 500 MiB of headroom are refused at the sixth, not after all twenty have filled the disk.

### Cleanup

One sweep every ten minutes, on every node (each step is idempotent): delete uploads whose `updated_at` is older than `files.abandoned_after_hours` along with their pieces, then `ReclaimOrphans` with the same age. The orphan sweep already spares calendar covers. A sweep failing is logged and retried next time.

### Files in federated messages

**Descriptors in the signed content.** A message with files carries `attachments: [{id, filename, contentType, size, sha256, width?, height?, caption?}]` in `message.sent` content, for every message, local or federated. One path, and the SHA-256 is then signed by the author's server — so a server copying the bytes from anywhere can check them against something the author vouched for. The type and name are claims; the receiving server types the bytes again when it copies them and refuses a blocked type.

**Rows on the receiving server.** When a home server accepts a remote post, and when a replica accepts a delivery, each descriptor becomes an `attachments` row with `origin_server` set to the event's origin, `storage_key` the descriptor's digest (validated as 64 hex characters), a fresh local id (never the remote one, so a peer cannot choose an id that collides with a local file), no uploader account, and `fetched_at` empty. The author's own server, receiving its own message back, binds its existing upload rows instead (by id, uploader and room). Clients need nothing new: the attachment's URL is the local `/api/attachments/:id`.

**Copied on first view, from the home server.** A download or preview of a row whose bytes are not here copies them first: from the room's **home server** if this server is a replica, or from the event's **origin** if this server is the home. The home server is the hub of a room already — it is the only server every participant talks to — so a replica asking the home server, and the home server asking the author's server, follows authorisation that already exists (`room_peers`). A replica asking another replica directly would need a membership check no replica can make.

`GET /api/federation/rooms/:slug/files/:digest` is signed like every federated request. The serving server answers only for a federated room the requesting peer participates in, and only for a file a sent message in that room carries; a home server that does not hold the bytes yet copies them from the origin first (once — concurrent requests for one digest wait on the same copy). The body is the raw bytes. The copier reads at most the declared size + 1 against `files.federation_copy_max_bytes`, checks length and SHA-256, re-detects the type and applies this server's type limits and the server-wide storage limit, then stores and makes a preview. A failure leaves the row uncopied and answers 502 with a reason, so the next view tries again.

**Who pays.** The reader's server pays disk for what its members look at; the home server pays for every file in rooms it hosts that anybody elsewhere looks at; the author's server pays for the original. Bandwidth: one copy per server per file, not per reader. A file larger than a server's copy limit is shown with its name and size and says it is too large to copy here.

**Deletion.** A message deleted on its home server sends `message.deleted`; a replica accepting one removes the copies the target carried. A local redaction on a replica already removes its rows.

**Backup.** A copy of another server's file is not backed up (the archive follows rows with no `origin_server`); after a restore, a missing copy is fetched again on the next view, because "row says copied, store says missing" is treated as "not copied".

### Direct transfers

**What it is.** A WebRTC data channel between the two devices, carrying the file in 64 KiB messages. Negotiated over the existing live connection with a new `transfer` action, beside `signal`, and the call ICE configuration from `GET /api/transfers/ice` (same servers; the TURN relay only when `files.direct_transfer_relay` is on). No new signalling server — conduit was looked at and brings its own; only its idea of keeping received pieces as Blobs is borrowed.

**Offers are rows.** `POST /api/rooms/:id/transfers {recipient, filename, size, contentType}` records an offer in `direct_transfers` (sender, recipient, room, what, state, expiry of ten minutes) — a row rather than memory because the two people may be connected to different nodes. Both must be members of the room; the recipient must not have blocked the sender. The recipient gets `transfer.offered`; `POST /api/transfers/:id/accept|decline|cancel|finish|fail` move the state, each telling both people with `transfer.updated`. Signals are relayed only while the offer is accepted, only between its two people, re-authorised on every frame as calls are. Rows are swept an hour after they end.

**What the recipient sees.** A card in the corner: who, the file name and size, "Accept" and "Decline", and the warning the spec requires — that their network addresses are visible to each other and to any relay. Accepting asks where to save where the browser can (File System Access API: written straight to disk, any size); elsewhere the pieces are kept as Blobs and offered as a download at the end, which browsers back with disk. Progress, speed and a cancel button on both sides.

**Backpressure.** The sender waits on `bufferedAmountLowThreshold` (1 MiB) whenever `bufferedAmount` passes 4 MiB, so a fast disk cannot queue the file in memory. The recipient acknowledges every 4 MiB and the sender stops when it is 16 MiB ahead, so a slow writer on the far side bounds the sender too.

**Resume: no.** A failure part way fails the transfer and offers the sender an ordinary upload of the same file; the upload is resumable, the data channel is not worth making so. **Fallback** is offered the same way when the other person is offline (the server says so when the offer is made), declines, or does not answer in ten minutes.

**Where it is offered.** In a direct conversation with one other person, from the attach menu. The API takes any member of any room, so a group can follow without a protocol change. Not to people on other servers. The feature is `direct-transfers`, on by default; it needs nothing configured, but without STUN only people on one network connect, and without TURN people behind strict NAT do not — which the client explains when it fails.

**Relay cost.** A relayed transfer passes through the TURN server twice (in and out), so a 2 GB file costs the operator 4 GB of relay traffic and no disk. An upload of the same file costs 2 GB in, 2 GB out per reader, and 2 GB of disk until deleted.

### Privacy and safety

Every existing download rule applies to copied files. Pieces are never addressable as attachments. Filenames are reduced as before (last path segment, control characters dropped, 200 bytes). A direct transfer's name and size are what the sender claims; the recipient's client saves rather than opens, and the received file is never rendered inline. The server sees that a transfer was offered, between whom, and its name and size, but never its bytes.

### Desktop

Pieces go through the existing JSON `Send` binding (base64), which gives the desktop real progress for the first time and keeps memory to one piece. Saving a file streams through a new `SaveMedia` binding behind a native save dialog instead of `FetchMedia`'s 64 MiB in-memory copy. WebKitGTK, WebView2 and WKWebView all carry `RTCDataChannel` (calls already depend on `RTCPeerConnection` there).

### Payload budget

The upload loop, the transfer host and the data-channel code are dynamically imported: the transfer host is a lazy component behind the capability, and the upload loop is imported on first upload. Nothing new is in the blocking bundle beyond a few lines in the media plugins.

## Risks / Trade-offs

- **Declared sizes are reserved for up to a day.** Somebody starting and abandoning many uploads holds quota until the sweep. Bounded by their own quota and by the per-file limit; uploads in progress per person are capped at 20 (twice the files one message may carry).
- **A home server pays for every file in its rooms that anybody elsewhere opens.** That is the cost of hosting a room; documented, and bounded by the copy limit.
- **First view of a large federated file waits for the copy.** The client shows a fetching state; the copy is one per server.
- **Direct transfers without TURN fail for many home connections.** The fallback to an upload is always offered.
