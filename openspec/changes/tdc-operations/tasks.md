Scope note: these tasks implement `platform/operations`. Tasks reference the spec for what to build and `design.md` for how.

**Documentation rule:** the hosting guide's backup, restore and restart sections, the upgrade guide and the integration guide are rewritten as the work is done.

**Phasing rule:** every phase ends in a running, demonstrable system, and a server whose administrator uses none of this behaves exactly as before.

## 1. Phase 1 — Backups

- [x] 1.1 Migration: `backups`, `operations` (one running backup or restore, by partial unique index), `server_nodes`, `maintenance`; permissions `server.backup`, `server.restore`, `server.restart`; settings `backup.interval_hours`, `backup.keep`, `backup.max_bytes`; configuration `backup.passphrase`, `backup.work_directory`, `server.restart_method`
- [x] 1.2 `internal/backup`: parts and the excluded list, with a test that every table is one or the other; dependencies between parts; channel selection
- [x] 1.3 Export from one `REPEATABLE READ` snapshot, streamed per table through spooled NDJSON into tar, gzip and age; files from the store; manifest and summary
- [x] 1.4 Jobs: claim, progress and heartbeat, cancel, stale jobs marked failed by any node; archives stored in the backend; list, download with `Range`, delete; chunked upload, joined across nodes
- [x] 1.5 Scheduled backups with retention, when a passphrase is configured
- [x] 1.6 API, audit and rate limits; API catalogue and integration guide
- [x] 1.7 Tests: archive unreadable without the passphrase; permission guards; parts closure

## 2. Phase 2 — Restores

- [x] 2.1 Generic restore from the catalogue: foreign-key order, required and optional references, self-references in a second pass, sequences
- [x] 2.2 Replace: one transaction, refuses an incomplete archive or another schema version; files back to the store; restart required
- [x] 2.3 Selective: settings (never the signing key), channels with files; refusals with reasons for the rest
- [x] 2.4 Preview: selective performed and rolled back; replace read through and compared
- [x] 2.5 Maintenance mode across nodes, health still up, live connections closed
- [x] 2.6 Tests: backup then restore into a fresh database gives equal data for every part; a deleted message comes back selectively; a failing replace changes nothing

## 3. Phase 3 — Reload and restart

- [x] 3.1 Reload by `SIGHUP` and API: log level, call relay secret, web shell, settings cache; report what needs a restart; refuse invalid configuration
- [x] 3.2 `tdc-server -check`; restart by `exec` or supervisor exit, detected or declared, refused when it would only stop the server; graceful drain; the new process records completion
- [x] 3.3 Node heartbeats; requests addressed to another node picked up by it; operations history
- [x] 3.4 Tests: restart refused on invalid configuration; refused without a supervisor under `supervisor`; permission guards

## 4. Phase 4 — Interface

- [x] 4.1 Backups page: parts with dependencies, chosen channels, passphrase, progress and cancel, list, download, delete, upload, restore with preview and confirmation
- [x] 4.2 Server page section: nodes, reload, restart with impact and confirmation, reconnect and report, recent operations
- [x] 4.3 English and Swahili; desktop through the shared layer; payload budget held
- [x] 4.4 Verified in a browser against a running server, restart under a supervisor loop and without one
