## Why

Running a TDC server today means a shell. Backing it up is `pg_dump` plus a `tar` of the attachments volume, restoring is `dropdb` and `pg_restore` with the server stopped, and picking up a changed configuration file — or the web assets after an upgrade — is a restart somebody does from the host. An operator who administers the community from the interface has to leave it, with root on the machine, for the three things most likely to matter on a bad day.

Neither of the manual procedures can take part of a server either: there is no way to keep a copy of one channel, of the settings, or of everything except the attachments, and no way to put back one channel's deleted history without restoring the whole database somewhere and copying rows by hand.

## What Changes

- **Backups from the interface and the API.** An administrator creates a backup of the whole server or of chosen parts — settings, accounts and roles, channels and messages (all, or chosen channels), the attachments' files, projects, the directory, integrations, the audit log. A part brings the parts it cannot stand without, so every archive restores.
- **A documented, versioned archive.** A tar stream holding a manifest and one newline-delimited JSON file per table, with the attachments' bytes beside them, taken from one consistent snapshot of the database. Always encrypted with a passphrase the operator chooses, in the [age](https://age-encryption.org) format, so a stolen archive is unreadable and the standard `age` tool can open one without TDC.
- **Kept on the server's storage, downloadable, or uploaded.** Archives live in the configured storage backend, are listed, downloaded (resumably) and deleted from the interface, and an archive kept elsewhere can be uploaded in pieces to restore from.
- **Restoring, with a preview first.** A whole-server restore replaces everything in one transaction. A selective restore puts back settings, or chosen channels' rows and files, adding what is missing and never overwriting what exists. Every restore can be previewed — a selective one is actually performed and rolled back, so the preview is what the restore would do — and runs with the server in maintenance mode. What cannot be restored partially is stated, with the reason.
- **Jobs that behave.** One backup or restore at a time across every node, claimed in the database; progress reported as it runs; cancellable; a job whose node died is noticed and marked failed. Scheduled backups with a retention count, when the operator has put a passphrase in the configuration.
- **Reload and restart.** Reload re-reads the configuration and the web assets and applies what can change in a running process (log level, the call relay secret, the application shell, settings) without dropping anybody, and says what still needs a restart. Restart validates the next process first — the same binary, the same configuration, the database it will use — and refuses with the problems listed if it would not start; otherwise it drains connections, closes cleanly, and replaces itself or exits for its supervisor, and the new process records that it came back. The administrator sees who is connected and which calls are running before confirming, and the page reports whether the server returned.
- **Nodes.** Each node announces itself in the database. Reload and restart target one node, from any node; an operator restarts several one at a time.

**Out of scope:** a member exporting their own data (a separate, personal feature, noted for later); restoring across schema versions (restore with the release that made the archive, then upgrade); automatic rolling restarts.

## Capabilities

### New Capabilities

- `platform/operations`: backups, restores, maintenance mode, reload and restart.

### Modified Capabilities

- `identity/authorization`: `server.backup`, `server.restore` and `server.restart`, administrative, held by administrators through `server.administer`.
- `platform/server-config`: `backup.interval_hours`, `backup.keep` and `backup.max_bytes` as settings; `backup.passphrase`, `backup.work_directory` and `server.restart_method` as configuration.

## Impact

- **Dependencies:** `filippo.io/age` (BSD-3-Clause) for encryption. Nothing else new.
- **Schema:** a migration adding `backups`, `operations`, `server_nodes` and `maintenance`. None of the four is in any backup: they describe the server's operation, not its community.
- **Operators:** archives are spooled to a working directory before storage, so the disk needs room for one archive and the largest table. A full restore signs everybody out and requires a restart, which the interface offers.
- **Security:** archives are never written unencrypted; the passphrase is never stored except where the operator put it in configuration for scheduled backups; every operation is permission-gated, rate-limited and audited before and after.
