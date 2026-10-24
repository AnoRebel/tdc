Scope note: these tasks implement the file-transfer requirements of `messaging/attachments` and the file parts of `federation/protocol`. Tasks reference the specs for what to build and `design.md` for how.

**Documentation rule: documentation is written as the work is done.** The user guide, the hosting guide and the integration guide stay current with each phase.

## 1. Files without text

- [x] 1.1 `rooms`: message and thread-reply variants that take the message's files; refuse only when there is neither text nor a file; descriptors in `message.sent` content
- [x] 1.2 HTTP: posting, thread replies and scheduled messages pass the files they bind; a scheduled message may be files alone
- [x] 1.3 Clients: the composer schedules a message that is only files; the empty-message refusal is gone from both clients' paths
- [x] 1.4 Tests against a database: files alone post in a channel, a thread and a direct conversation; neither text nor files is still refused; the guard broken and the test seen to fail

## 2. Limits, quotas and resumable uploads

- [ ] 2.1 Migration 000436: `file_uploads`; `attachments.origin_server` and `fetched_at`; `direct_transfers`; backup guard (both new tables excluded with reasons)
- [ ] 2.2 Settings: `files.max_file_bytes`, `files.max_message_bytes`, `files.quota_per_person_bytes`, `files.upload_piece_bytes`, `files.abandoned_after_hours`; the attachments service reads limits from them per request
- [ ] 2.3 Uploads in pieces: begin (membership, size, person and server quota, at most 20 in progress), piece (raw or base64, in order, bounded), status, finish (the single-upload path), abandon; only the uploader; catalogued, OpenAPI record regenerated
- [ ] 2.4 `GET /api/attachments/limits` gains the quota, what the person uses and the piece size; the server-wide limit counts uploads in progress
- [ ] 2.5 Sweep: abandoned uploads and their pieces, and files never sent, after `files.abandoned_after_hours`
- [ ] 2.6 Tests: a file in pieces arrives whole and identical; out-of-order and oversized pieces are refused; another person cannot see or add to an upload; quota and server limit refuse at begin; finish applies type limits; the sweep removes pieces and orphans. Guards broken in turn

## 3. Clients upload in pieces

- [ ] 3.1 A shared, lazily loaded upload loop: begin, send pieces with retry and backoff, resume from the server's count, finish, cancel
- [ ] 3.2 Web: pieces as raw bytes with per-piece progress; desktop: pieces as base64 through `Send`, with progress
- [ ] 3.3 Desktop: saving a file streams to disk through a save dialog
- [ ] 3.4 The composer shows the person's quota and refuses a file over it before sending

## 4. Files across servers

- [ ] 4.1 Remote posts carry attachment ids; the home server's remote-post handler records descriptors as remote rows; a replica records them on delivery, and the author's server binds its own
- [ ] 4.2 `GET /api/federation/rooms/:slug/files/:digest`, signed, participation-checked, copying from the origin when the home server does not hold the bytes
- [ ] 4.3 Copy on first view: from the home server (replica) or the origin (home); size cap, length and digest checked, type re-detected and limited, storage limit applied, preview made, one copy at a time per digest
- [ ] 4.4 Deletion arriving from the home server removes copies; backups skip copies and a missing copy is fetched again
- [ ] 4.5 Setting `files.federation_copy_max_bytes`
- [ ] 4.6 Tests with two servers: a file sent on one is seen and downloaded on the other byte for byte; a tampered digest is refused; a peer with no part in the room is refused; a file over the copy limit is refused clearly. Guards broken in turn

## 5. Direct transfers

- [ ] 5.1 Feature `direct-transfers`, setting `files.direct_transfer_relay`, `GET /api/transfers/ice`
- [ ] 5.2 Offers: create (both members, not blocked, says whether the recipient is online), accept, decline, cancel, finish, fail; frames to both people; expiry and sweep
- [ ] 5.3 The `transfer` live action: relayed only within an accepted offer, only between its two people, re-authorised per frame
- [ ] 5.4 Tests: only the recipient may accept; a stranger cannot signal; a signal before acceptance is refused; an expired offer cannot be accepted. Guards broken in turn
- [ ] 5.5 Client: the transfer host (lazy), the offer card with the address warning, save-to-disk where available, progress, cancel, backpressure, fallback to an upload
- [ ] 5.6 Offered from the attach menu in a direct conversation; English and Swahili

## 6. Documentation and checks

- [ ] 6.1 User guide: sending files, large files, files from other servers, direct transfers, what the other person sees
- [ ] 6.2 Hosting guide: the settings, a disk and bandwidth guide for 500 and 5,000 members, proxy body sizes, quotas, relay cost, federation storage, cleanup, backups
- [ ] 6.3 Integration guide: the upload protocol; webhooks: the `attachments` descriptors in `message.sent`
- [ ] 6.4 Payload budget measured; a browser check against a running server
