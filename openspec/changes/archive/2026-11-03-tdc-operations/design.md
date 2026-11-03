## Context

Configuration is read once in `cmd/tdc-server` (`config.Load`), which wires every service and calls `httpapi.Server.Run`; cancelling its context drains live connections, waits for background work and shuts the HTTP server down within `server.shutdown_timeout`. Settings live in `server_config`, cached for 30 seconds per node. Attachments are rows in `attachments` naming keys in a `storage.Store` (local directory or S3), whose `Put` needs the size up front. Several nodes may share one database (`server.multi_node`), with events crossing between them over `LISTEN`/`NOTIFY`. The audit recorder refuses rather than performs unrecorded. The database has about ninety tables, all `TIMESTAMPTZ`, two `bigserial` keys, six generated columns and two self-referencing foreign keys (`users.suspended_by`, `events.thread_root_id`); no other cycles.

## Decisions

### The archive is our own format, not a `pg_dump`

A `pg_dump` is the right tool for the whole database and stays documented for that. It cannot be the in-app format: the server image carries no `pg_dump`, a dump must match the Postgres major version, it cannot be cut by channel, and a data-only dump restores across foreign keys only as a superuser (`--disable-triggers`). The server needs a format it can write and read itself, as an ordinary database user.

An archive is:

```
age( scrypt passphrase )
  └─ gzip
       └─ tar
            manifest.json                 first, always
            tables/<part>/<table>.ndjson  one JSON object per row, as row_to_json writes it
            files/<storage key>           the bytes of each attachment and preview
            summary.json                  last: rows per table, files written, files missing
```

`manifest.json` says `format: "tdc-backup"`, `formatVersion: 1`, the schema version (the newest migration applied), the server's version and name, when it was taken, the parts, and the channels when only some were chosen. A reader refuses a format version it does not know and — for restoring — a schema version other than its own.

NDJSON per table because it streams in both directions, is readable by anything, and maps one to one onto `row_to_json` / `json_populate_recordset`, which handle every column type the schema uses without a line of per-type code. Each table is spooled to a temporary file and then written into the tar (whose headers need sizes); the archive itself is spooled before being handed to the store. Nothing holds a table in memory.

### One snapshot

Export runs inside one `REPEATABLE READ, READ ONLY` transaction, so every table is read as of the same instant: a message and its reactions, a role and who holds it. Files are read from the store during that transaction, by the keys the snapshot names; an attachment removed since (retention, deletion) is listed as missing in `summary.json` rather than failing the backup.

### Parts

A part is a set of tables. Every table in the schema is either in a part or listed as excluded with a reason, and a test fails on a table that is neither — the next migration cannot silently fall out of backups.

| Part | Tables | Needs |
| --- | --- | --- |
| `settings` | `server_config`, `server_statuses` | — |
| `accounts` | users, credentials, roles and grants, privacy, blocks, former names, sign-in links, push subscriptions, digests | — |
| `channels` | rooms and everything scoped to a room: members, events, reactions, pins, threads' search rows, lifetimes, views, interactions, notifications, bans, restrictions, invitations, scheduled messages, reports, federation peers, calls and call links, attachment rows | accounts |
| `files` | the stored bytes of the archived attachments | channels |
| `projects` | projects, issues and everything about them | accounts, all channels |
| `directory` | profiles, companies, skills, enquiries, engagements, proposals | accounts, all channels, projects |
| `integrations` | applications, tokens, incoming webhooks, subscriptions, deliveries | accounts, all channels |
| `audit` | `audit_entries` | accounts |

Excluded: sessions, sign-in ceremonies and pending sign-ins, recovery and confirmation tokens, OAuth outcomes, typing and presence, idempotency keys, schema bookkeeping, and this change's own tables. Each lapses, or describes the running server rather than the community.

Choosing a part chooses what it needs; choosing some channels rather than all is allowed only with parts that do not need all of them. Every archive therefore restores into an empty database.

### Secrets

Every archive is encrypted; there is no unencrypted option. The archive holds password hashes, second-factor secrets, webhook signing secrets and the server's signing key, and an option to leave encryption off would be the option somebody picks to make a download "easier". age with an scrypt recipient: a passphrase-based format with a published specification, an audited Go implementation under BSD-3-Clause, and a standard command-line tool (`age -d`) that opens an archive without TDC. The passphrase must be at least twelve characters, is used and forgotten, and appears in no log, audit entry or response. A wrong passphrase is reported as wrong, never as corruption.

Scheduled backups need a passphrase nobody types, so they use `backup.passphrase` from the configuration — not a setting, which would put it in the database the archive protects.

### Restore: replace, or add what is missing

**Replace** (the whole server): the archive must hold every part and every channel, at the same schema version. In one transaction every backed-up and excluded table is truncated and each table inserted in foreign-key order; self-referencing columns are inserted empty and filled in a second pass; `bigserial` sequences are moved past the restored rows. A failure anywhere rolls back to exactly what was there. Files are then written back to the store. Every session is gone afterwards, and the server's signing key may have changed under a running process, so the operation ends *restart required*, and maintenance mode stays on until a node restarts.

**Selective**, from any archive holding the part:

- *Settings*: each stored key is written; the archive's value wins; the signing key is never touched.
- *Channels* (all in the archive, or chosen ones), with their files if the archive has them: rows are inserted with `ON CONFLICT DO NOTHING`, so nothing existing is overwritten and a deleted message comes back. A row whose required reference (a sender's account, its channel) is not in the database is skipped; an optional reference to something missing is cleared. A channel whose name is now taken by another is skipped with everything in it, and the preview says so. Files are written only where the store has none.
- Accounts, projects, the directory, integrations and the audit log are **not** restorable partially. Accounts interlock with every grant, session and membership; projects and the directory reference rooms, issues and each other in ways a subset breaks; integrations carry tokens whose meaning depends on the accounts they belong to; the audit log is append-only, and inserting old entries among new ones would misnumber both. These come back only with a replace — or by adding them to a server that holds none of that part yet, since every reason above is about mixing an archive's rows with a server's own. Into an empty server the signing key is restored too.

**Preview.** A selective restore is previewed by performing it in a transaction and rolling back, so the counts shown — rows in the archive, rows that would be added, rows skipped and why — are what the restore would do. A replace is previewed without touching the database: the archive is read through, and its counts are shown beside the current ones.

### Generic, from the catalogue

Restore asks Postgres for each table's columns (skipping generated ones), primary key, serial sequences and foreign keys, and builds one statement per table:

```sql
INSERT INTO t (cols)
SELECT <cols, an optional reference cleared when its target is missing>
FROM json_populate_recordset(NULL::t, $1) r
WHERE <every required reference exists>
ON CONFLICT DO NOTHING
```

run over batches of 500 rows. Nothing names a column by hand, so a migration adding one needs nothing here.

### Jobs

A backup or restore is a row in `operations` (`kind`, `status`, `node`, `progress`, `heartbeat_at`, `cancel_requested`). A partial unique index allows one running backup or restore across every node; a second request is refused with the one in progress named. The running node updates progress and heartbeat every few seconds and checks `cancel_requested`; cancelling rolls back. Any node that finds a job whose heartbeat is older than two minutes marks it failed, so a crashed node does not hold the lock for ever. Jobs are not resumable: a resumed snapshot would not be one snapshot. Downloads are — the endpoint honours `Range`, and an S3 store hands out a presigned URL.

Uploading an archive to restore from is done in pieces of up to 8 MB through the ordinary API, each written to the store as its own object and joined when the last arrives, so it works through either client's transport and across nodes behind a load balancer, and can be resumed from the last piece the server holds.

### Maintenance mode

A single row in `maintenance`. While it is on, every API request is answered `503` with code `maintenance` except health, the server's identity, the signed-in person, and the operations and backup endpoints an administrator watches; live connections on the node starting the restore are closed. Other nodes read the row with a two-second cache, and a restore waits three seconds after turning it on before it writes. Health stays `200` during maintenance, so an orchestrator does not kill the node doing the restore.

### Reload

`SIGHUP` or the interface. Reload reads the configuration again, validates it, and applies what a running process can change: `log.level` (an `slog.LevelVar`), `calls.turn_secret`, the web application shell from `server.web_root`, the configuration shown to administrators and its warnings; it also drops the settings cache. Every other changed value is reported as needing a restart. An invalid configuration changes nothing and is reported with every problem. A container configured by environment sees no change on reload: its environment is fixed at creation.

### Restart

`server.restart_method`: `auto` (default), `exec`, `supervisor` or `off`.

- `exec` — after a graceful shutdown the process replaces itself (`syscall.Exec`) with the binary at its own path, same arguments and environment. The process identifier is kept, so a container's main process or a systemd unit is undisturbed, and nothing outside is needed.
- `supervisor` — after a graceful shutdown the process exits with status 75 and its supervisor starts it again. Refused unless a supervisor is detected (systemd's `INVOCATION_ID` or `NOTIFY_SOCKET`) or the operator chose this method explicitly; otherwise restarting would only stop the server.
- `auto` — `supervisor` under systemd, `exec` otherwise. `off` refuses.

Before anything stops, the server runs its own binary as `tdc-server -check` with the same arguments and environment: it loads the configuration, connects to the database, and checks the schema is one it can run against. A non-zero exit refuses the restart and returns what it printed. Then the operation is recorded and audited, the response is sent, and shutdown begins. The new process is told which operation it completes (`TDC_RESTART_OPERATION` in its environment under `exec`; by node host under a supervisor), records it succeeded once it is listening, and audits that. The page polls the operation and `/api/health`, and reports success with the new version and start time, or after ninety seconds that the node did not come back and where to look.

### Nodes

Each node writes a row in `server_nodes` on start and every fifteen seconds: its identifier, host, version, start time, live connections. Rows unseen for a minute are shown as gone and removed after a day. Reload and restart name a node; the node a request reaches acts at once if it is the one named, and otherwise records the request, which the named node picks up within five seconds. Restarting several nodes is done one at a time, waiting for each — documented, not automated.

### Permissions, limits, audit

`server.backup` (create, list, download, delete, upload, schedule), `server.restore` (preview and restore), `server.restart` (reload, restart, nodes, operations history). All three administrative. Creating, uploading, restoring and restarting are rate-limited per person. `backup.max_bytes` bounds an archive being written or uploaded (zero is no limit, as with the other limits). Audit entries: `server.backup.created`, `.failed`, `.downloaded`, `.deleted`, `.uploaded`; `server.restore.started`, `.completed`, `.failed`; `server.reload`; `server.restart.requested`, `.refused`, `.completed`; `server.maintenance.changed`. A replace restore writes its completion entry inside the restored log, in the same transaction.

## Risks

- **A long snapshot holds back vacuum** for its duration. Acceptable for a backup that takes minutes; stated in the hosting guide.
- **A replace restore is one transaction** and writes as much WAL as the data it restores.
- **`exec` cannot recover from a new binary that fails after `-check` passed** (a port taken in between). Without a supervisor the server is then down, which the confirmation says before anybody clicks.
- **Maintenance reaches other nodes within two seconds**, not instantly; the restore's three-second wait covers it.
