# Upgrading a TDC server

What happens when you move a running server from one release to another: the
ordinary case, the case where you have skipped several releases, what rolling
back does and does not do, and what to do when a migration fails partway.

This assumes the Compose deployment described in [hosting](hosting.md).

## The ordinary upgrade

```sh
docker compose pull && docker compose up -d
```

That is the whole of it. There is no separate migration step, no maintenance
mode to enter, and nothing to run by hand.

The reason it can be this short is that the binary carries its own schema. The
migrations are embedded in it at build time, so a released server knows exactly
which schema it expects and never has to be pointed at a directory of files that
might not match. On start, before the connection pool opens and before it
answers anything, it compares what the database is at with what it expects and
applies whatever is missing.

Watch the log the first time:

```sh
docker compose logs -f server
```

A server with nothing to do says `database schema up to date`. One with work to
do says `applying migrations` with the versions it is moving between, then
`migrations applied`. Either way the server starts answering afterwards, and if
it does not, the log says why rather than the server silently running against a
schema it does not understand.

### Take a backup first

Every time, not only for the upgrades that look risky.

```sh
docker compose exec -T database \
  pg_dump -U tdc -d tdc --format=custom --file=/tmp/tdc.dump
docker compose cp database:/tmp/tdc.dump ./before-upgrade.dump
docker compose exec -T database rm /tmp/tdc.dump
```

The reason is in the next section: a schema change is not automatically
reversible, and the dump is what makes going back possible at all. It takes
seconds on a community-sized database, and the one time it is needed it is the
only thing that helps.

## Skipping releases

**Upgrading across several releases at once is supported and needs nothing
special.** A server that has been left alone for six releases applies every
pending migration in order on its next start. There is no need to step through
the releases one at a time, and nothing is gained by doing so.

This is a property of how migrations are applied rather than a claim about any
particular pair of versions. The runner reads the schema version the database is
at, compares it with the highest version the binary carries, and applies
everything in between in sequence. Whether that gap is one migration or twenty
makes no difference to the mechanism.

Each migration runs in its own transaction wherever Postgres allows it, so a
failure part way through the sequence leaves the schema at the last version that
completed, rather than half-applied. That is what makes a long jump safe to
attempt: a failure at migration fourteen of twenty leaves a database at thirteen
and a clear message, not a database in a state nothing understands.

Every migration is also checked against a genuinely empty database in continuous
integration, applied twice to confirm the runner is idempotent. A migration
written against whatever schema its author happened to have is the classic way
an upgrade breaks for everybody else, and applying the whole sequence from empty
is the only thing that catches it.

The practical advice for a long jump is unchanged: take the backup, read the
release notes for anything between where you are and where you are going, and
watch the log on the first start.

## Rolling back

An older binary **refuses to start** against a database migrated by a newer one:

```
database schema is newer than this binary: database is at version 21, this binary expects 18
```

That refusal is the feature. Running an old build against a schema it does not
understand means operating on columns it does not know about and constraints it
does not respect, and the damage that does is quiet and cumulative. Declining is
the behaviour that makes a rollback safe to attempt at all — you find out
immediately rather than a week later.

It also means a rollback across a schema change is not a matter of pulling the
old image. Two situations, and they are very different:

**If the upgrade added no migration**, the old image starts against the current
database and there is nothing more to it. Pin the previous tag in the compose
file and bring it up.

**If the upgrade added a migration**, going back means restoring the database
from the backup taken beforehand, and then starting the old image against it.
The [restore procedure](hosting.md#restore) is the one to follow. Anything
written between the backup and the rollback is lost — that is the cost, and it
is why the backup is taken immediately before the upgrade rather than nightly.

The migrations do carry `down` files, and they are correct. But the server never
applies them: `Migrate` only ever goes up, and there is no command that steps a
schema backwards. Restoring from a dump is the supported route, and it is the
one to plan for.

## When a migration fails

A migration that fails leaves the database marked dirty, and the next start
refuses with:

```
database is marked dirty at version 14: a previous migration failed and needs manual inspection
```

The server will not continue past that, and it is right not to: applying the
next migration on top of a half-applied one is how a schema becomes something
nobody can reason about. It wants a person to look.

What to do, in order:

**Read the log from the failed run.** The error from the migration itself is
there, and it is usually specific — a constraint that existing data violates, a
column that already exists because of a previous half-run, a permission the
database user does not have.

**Restore from the backup** if you have one from immediately before, which is
the quickest route back to a known state. Then work out what happened before
trying again.

**If you do not have one**, the database is at a known version with one
migration partly applied. Inspect what that migration does — the files are in
`server/internal/db/migrations/`, and the one that failed is the version after
the number in the message. Undo its partial effects by hand, clear the dirty
flag in the `schema_migrations` table, and start again. This is genuinely manual
work and is the case the backup exists to avoid.

Either way, the cause is worth reporting. A migration that fails on a real
database and not in CI is a migration with an assumption in it about data, and
that is worth fixing for everybody rather than only working around locally.

## The web assets

One upgrade-adjacent thing that looks like a networking fault and is not.

The application shell is read once when the server starts and served from memory
on every navigation afterwards. It carries the configuration the application was
built with. So a server still running with an old shell in memory serves an
application that calls the address it was built for rather than the one it is
served from, and the browser shows requests going somewhere unexpected.

`docker compose up -d` recreates the container and therefore rereads it, so the
ordinary upgrade is unaffected. It matters when web assets are replaced
underneath a running server by any other route: restart the server afterwards.

## Upgrading Postgres itself

The compose file runs `postgres:18`, and a Postgres major-version upgrade is its
own operation — the data directory format changes between majors, and a new
major will not start against an old one's directory.

The compose file is already arranged to make it possible. Postgres 18 moved its
data to a version-specific directory and moved the declared volume up to
`/var/lib/postgresql`, and the file mounts that parent rather than the data path
inside it. Mounting the old path instead leaves the container writing to an
anonymous volume and failing to start, which is a confusing way to discover the
difference.

The route that always works, and the one to use unless you have a reason
otherwise:

1. Take a dump with the running major, as above.
2. Stop the stack and remove the database volume.
3. Change the image tag in `compose.yaml`.
4. Bring up only the database, and restore the dump into it.
5. Bring up the rest.

`pg_upgrade --link` is faster and the volume layout supports it, but it operates
in place and a mistake is not recoverable without the dump you would have taken
anyway. For a community-sized database the dump-and-restore takes minutes and
cannot leave you worse off.

## A checklist

For an upgrade you want to be able to walk away from:

1. Read the release notes for everything between your version and the target.
2. Back up the database, and the attachment volume if it has changed since the
   last one.
3. Note the version you are on — `curl https://your-server/api/version`.
4. `docker compose pull && docker compose up -d`.
5. Watch `docker compose logs -f server` until it reports the schema applied or
   up to date and begins serving.
6. Check `/api/health` answers 200.
7. Sign in and read a message. The health check knows the database answers; it
   does not know the application works.
