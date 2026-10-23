## Purpose

Defines how an administrator keeps a TDC server safe and running from the interface: taking and restoring backups of all or part of it, the maintenance mode a restore runs under, and reloading or restarting the server process — each permission-gated, audited, and safe on a server whose nodes share one database.

## Requirements

### Requirement: Backups of chosen parts

An administrator holding `server.backup` SHALL be able to create a backup of the whole server or of chosen parts: settings, accounts and roles, channels and messages (all channels or chosen ones), attachment files, projects, the directory, integrations, and the audit log.

Choosing a part MUST also include every part it depends on, so that every archive can be restored into an empty database. Every table in the schema MUST belong to a part or be explicitly excluded as transient or operational.

#### Scenario: A part brings what it needs

- **WHEN** an administrator chooses only channels and messages
- **THEN** the backup also contains accounts, and the manifest lists both parts

#### Scenario: Chosen channels only

- **WHEN** an administrator chooses two channels rather than all
- **THEN** the archive holds those channels' rows and no other channel's

#### Scenario: A new table cannot fall out of backups

- **WHEN** a migration adds a table that is neither in a part nor excluded
- **THEN** the test suite fails

### Requirement: A consistent, documented, versioned archive

A backup SHALL be one archive in a documented format: a manifest naming the format version, the schema version, the server and its version, the time, and the parts; one newline-delimited JSON file per table; the bytes of included files; and a closing summary of what was written and what was missing.

All tables MUST be read from a single consistent snapshot of the database. The archive MUST be produced by streaming, without holding a table in memory.

#### Scenario: Rows written during a backup

- **WHEN** a message is posted while a backup is being taken
- **THEN** the archive contains either the message and everything about it, or none of it

#### Scenario: A file deleted during a backup

- **WHEN** an attachment's file is gone from storage when the backup reaches it
- **THEN** the backup completes and the summary names the file as missing

### Requirement: Archives are always encrypted

Every archive SHALL be encrypted with a passphrase the administrator supplies, of at least twelve characters, using the age format with a scrypt recipient. No archive, and no part of one, MUST ever be written unencrypted to storage or sent unencrypted to a client. The passphrase MUST NOT be stored, logged, audited or returned.

#### Scenario: Without the passphrase

- **WHEN** an archive is opened with a wrong passphrase
- **THEN** nothing in it can be read and the error says the passphrase is wrong

#### Scenario: Readable without TDC

- **WHEN** an operator runs the standard `age -d` on an archive with its passphrase
- **THEN** the result is the documented tar stream

### Requirement: Archives are kept, listed, downloaded and deleted

Archives SHALL be stored in the server's configured storage backend and listed with their parts, size, creator, time and status. An administrator holding `server.backup` SHALL be able to download one, with interrupted downloads resumable, to delete one, and to upload an archive kept elsewhere, in pieces that can be resumed.

Archive size MUST be bounded by `backup.max_bytes` when it is set, both when writing and when uploading.

#### Scenario: Uploading from elsewhere

- **WHEN** an administrator uploads an archive made by another server of the same version
- **THEN** it is listed and can be previewed and restored like one made here

### Requirement: Scheduled backups with retention

When the configuration holds `backup.passphrase` and `backup.interval_hours` is greater than zero, the server SHALL take a full backup at that interval and keep the newest `backup.keep` scheduled archives, deleting older scheduled ones. Archives created by hand MUST NOT be deleted by retention.

#### Scenario: Retention

- **WHEN** a scheduled backup completes and more than `backup.keep` scheduled archives exist
- **THEN** the oldest scheduled archives beyond that number are deleted

### Requirement: Restoring the whole server

An administrator holding `server.restore` SHALL be able to replace everything on the server with an archive holding every part and every channel, made at the same schema version.

The replacement MUST happen in one transaction, so that a failure leaves the server exactly as it was. Afterwards every session MUST be ended and a restart MUST be required before the server leaves maintenance mode.

#### Scenario: A failing restore changes nothing

- **WHEN** a replace restore fails part way through
- **THEN** every table holds what it held before the restore began

#### Scenario: An archive from another schema version

- **WHEN** an archive was made at a different schema version
- **THEN** the restore is refused and says which release made it

### Requirement: Restoring part of a server

An administrator holding `server.restore` SHALL be able to restore settings, or all or chosen channels with their messages and files, from any archive that holds them.

A selective restore MUST only add what is missing and MUST NOT overwrite existing rows, except settings, where the archive's value replaces the current one and the server's signing key is never changed. Rows whose required references are missing MUST be skipped, and the number skipped reported. Accounts, projects, the directory, integrations and the audit log MUST NOT be restorable partially into a server that already holds any of that part, and the interface MUST say so and why.

#### Scenario: A deleted message comes back

- **WHEN** a channel's messages are restored from an archive taken before one was deleted
- **THEN** the deleted message is present again and no existing message has changed

#### Scenario: Restoring accounts alone is refused

- **WHEN** a selective restore of accounts is requested
- **THEN** it is refused with the explanation that accounts are restored only by replacing the whole server

### Requirement: Restores are previewed

Every restore SHALL be previewable without effect. The preview of a selective restore MUST report, per table, the rows in the archive, the rows that would be added and the rows that would be skipped, as computed by performing the restore and rolling it back. The preview of a replacement MUST report the archive's contents beside the server's current ones.

#### Scenario: Preview matches the restore

- **WHEN** a selective restore is previewed and then performed with nothing changed in between
- **THEN** the rows added are the rows the preview said would be added

### Requirement: Maintenance mode during restore

Applying a restore SHALL put the server into maintenance mode on every node before anything is written. In maintenance mode, API requests other than health, server identity, the signed-in person, and the backup and operations endpoints MUST be refused with an explanation, and health MUST still report the server as up.

#### Scenario: Posting during a restore

- **WHEN** a member posts while a restore is being applied
- **THEN** the post is refused saying the server is in maintenance

### Requirement: Operations are jobs that can be watched and stopped

Backups and restores SHALL run as jobs, one at a time across all nodes, reporting progress as they run. A job MUST be cancellable, and cancelling MUST leave nothing half-applied. A job whose node stopped MUST be marked failed by another node within minutes rather than blocking further jobs.

#### Scenario: Two at once

- **WHEN** a backup is requested while another backup or restore is running on any node
- **THEN** the request is refused and names the running operation

### Requirement: Reload without disconnecting

An administrator holding `server.restart` SHALL be able to reload a node: re-read the configuration, validate it, and apply what can change in a running process — the log level, the call relay secret, the web application, and settings — without closing any connection. The result MUST list what was applied and what changed but needs a restart. An invalid configuration MUST change nothing and MUST be reported with every problem. Sending the process `SIGHUP` SHALL do the same.

#### Scenario: Invalid configuration on reload

- **WHEN** a reload finds the configuration file names an unknown storage backend
- **THEN** nothing changes and the administrator sees the problem

### Requirement: Restart that cannot take the server down by mistake

An administrator holding `server.restart` SHALL be able to restart a node. Before stopping, the server MUST run its own binary to check the configuration and the database it will start with, and MUST refuse with the problems found if the check fails. It MUST refuse when the restart method would only stop the process, stating why. Before confirming, the administrator MUST be shown how many people are connected and how many calls are in progress.

A restart MUST drain connections and shut down gracefully, then come back by replacing the process or by its supervisor. The restarted process MUST record that the restart completed, and the administrator's page MUST report whether the node returned, with its version and start time, or that it did not within a stated time.

#### Scenario: A bad configuration refuses the restart

- **WHEN** a restart is requested while the configuration file is invalid
- **THEN** the server keeps running, the restart is refused, and the problems are listed

#### Scenario: Restart completes

- **WHEN** a valid restart is confirmed
- **THEN** the node comes back, records the restart completed, and the page reports success

### Requirement: Nodes are visible and addressed

Every node SHALL record itself and a regular heartbeat in the database. An administrator SHALL see the nodes, and SHALL be able to reload or restart a chosen node from any node.

#### Scenario: Restarting another node

- **WHEN** an administrator connected through node A restarts node B
- **THEN** node B restarts and node A keeps serving

### Requirement: Operations are permission-gated, limited and audited

Backups need `server.backup`; restores and their previews need `server.restore`; reload, restart and node and operation history need `server.restart`. All three SHALL be administrative and held by administrators. Creating, uploading, restoring, reloading and restarting MUST be rate-limited per person. Each MUST be recorded in the audit log when requested and when it completes or fails.

#### Scenario: Without permission

- **WHEN** somebody without `server.backup` requests a backup
- **THEN** it is refused naming the permission, and nothing is created
