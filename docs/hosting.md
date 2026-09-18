# Hosting a TDC server

Everything needed to run a TDC server for other people: getting it up, putting
TLS in front of it, backing it up, restoring it, and keeping it current.

The premise throughout is that this costs nothing to run. A working server needs
Docker and a machine; it needs no account with anybody, no API key and no paid
service. Everything optional is genuinely optional, and the server starts and
behaves correctly with all of it switched off, which is the default.

## What you need

A machine with Docker and the Compose plugin, and somewhere between one and two
gigabytes of memory to give it. The server itself targets under a gigabyte;
Postgres wants its own share, and attachments want disk rather than memory.

A domain name, if anybody outside the machine is going to use it. TLS needs one,
and so does the canonical address.

## Getting it running

```sh
git clone https://github.com/AnoRebel/tdc
cd tdc
docker compose up -d
```

That is the whole product on <http://localhost:8080>: the web application and
the API on one port, with Postgres behind them. The schema, the attachment store
and the server's signing key are all created on first start, so there is nothing
to prepare beforehand.

One origin is deliberate rather than convenient. The session cookie is HttpOnly
and `SameSite=Lax`; served from a different host to the API, a browser treats it
as third-party and declines to send it back. Serving both from one port also
means no CORS preflight on every request and one thing to expose.

Open it in a browser. A server with no accounts leads to first-run setup, which
creates the administrator. That path closes the moment an account exists, so it
cannot later be used to grant administration to somebody who turns up
afterwards, and there is no default credential at any point.

### Before anybody else uses it

The shipped `compose.yaml` runs unedited so that you can look at the thing
before committing to it. Four values in it are marked `CHANGE ME`, and all four
matter before anybody else connects.

**`TDC_SERVER_CANONICAL_ADDRESS`** — how this server is known. Set it to the
name people will actually type, `chat.example.org`. It is never inferred from a
request header, which is what keeps it correct behind a proxy, and it is the
host part of every `user@server` address the server hands out. Left unset, the
server warns at startup and calls itself `localhost`.

**The database password** — it appears twice, in `POSTGRES_PASSWORD` and inside
`TDC_DATABASE_URL`, and both must agree. The shipped value is published in a
file in a public repository and is therefore not a secret.

**`TDC_SERVER_SECURE_COOKIES`** — the compose file sets it to `false`, because
the local stack is plain HTTP and a `Secure` cookie there would be set and then
never sent back. **Remove that line for any deployment served over HTTPS.**
Leaving it means session cookies travel in the clear and can be read by anybody
on the same wire.

**`TDC_SERVER_ALLOWED_ORIGINS`** — set in the shipped file for local
development, where the web app runs on port 3000. A normal deployment serves the
app from the same origin as the API and wants this empty.

Two more things to do to the file for a real deployment. Remove the published
port on the database — the server reaches it over the compose network and
nothing outside the host needs to. And decide what to do about the server's own
port: with a reverse proxy in front, bind it to loopback with
`127.0.0.1:8080:8080` so that nothing reaches the server except through the
proxy.

## Configuring it

Settings come from a file or from the environment, and the environment wins. An
environment variable is the setting's name in capitals with dots as underscores
and a `TDC_` prefix: `storage.backend` is `TDC_STORAGE_BACKEND`.

The container is configured entirely by environment; it has no config file and
no flag for one. Running the binary outside a container, `-config <path>` names
a file, whose format is one `section.key = value` per line with `#` for
comments. It is optional — environment alone is enough.

**Every problem is reported at once.** A configuration with five mistakes in it
names all five and refuses to start, rather than making you restart five times
to find them one at a time. Secrets are redacted wherever configuration is
displayed, so a database URL shows its host and database name and not its
password.

The settings an operator is most likely to touch:

| Setting                    | Default              | What it does                                         |
| -------------------------- | -------------------- | ---------------------------------------------------- |
| `server.canonical_address` | —                    | How this server is known. Set it.                    |
| `server.address`           | `:8080`              | What to listen on                                    |
| `server.name`              | `TDC`                | What it calls itself                                 |
| `server.secure_cookies`    | `true`               | Session cookies only over TLS                        |
| `server.session_lifetime`  | `720h`               | How long a session lasts unrenewed                   |
| `server.keepalive`         | `45s`                | How often an idle live connection is pinged          |
| `server.allowed_origins`   | —                    | Browser origins allowed to call the API cross-origin |
| `server.default_language`  | `en`                 | Interface language for somebody who has not chosen   |
| `database.url`             | —                    | Required; `postgres://…`                             |
| `database.auto_migrate`    | `true`               | Apply pending migrations at startup                  |
| `storage.backend`          | `local`              | `local`, `s3-aws` or `s3-minio`                      |
| `storage.directory`        | `./data/attachments` | Where files live under `local`                       |
| `log.level`                | `info`               | `debug`, `info`, `warn`, `error`                     |
| `log.format`               | `json`               | `json` or `text`                                     |

Two settings exist for local development and should never be on in a deployment
anybody else uses: `server.expose_internal_errors`, which returns internal error
detail to callers, and `server.secure_cookies = false`. The server warns about
both at startup, along with an unset canonical address and a database URL with
TLS disabled. Read the startup log the first time; it is telling you what it
thinks is wrong.

Attachment storage, malware scanning, the media sidecar, link previews and GIF
search are all covered in the repository's [README](../README.md), which is the
reference for what each costs and what is lost by leaving it off. The short
version is that only link previews reaches outside the machine by default, and
that can be turned off too.

### Signing in through another service

Nothing is configured by default: a stock server signs people in with a username
and a password and involves nobody else. Providers are added by naming them in
`oauth.providers`, and each name opens a namespace of its own — a provider
called `forgejo` is configured under `oauth.forgejo.*`, which is
`TDC_OAUTH_FORGEJO_*` in the environment.

| Setting                      | What it is                                         |
| ---------------------------- | -------------------------------------------------- |
| `oauth.providers`            | Comma-separated names, e.g. `forgejo,github`       |
| `oauth.<name>.label`         | What appears on the button; defaults to the name   |
| `oauth.<name>.client_id`     | The application identifier the provider issued you |
| `oauth.<name>.client_secret` | The secret it issued you                           |
| `oauth.<name>.auth_url`      | Where somebody is sent to approve                  |
| `oauth.<name>.token_url`     | Where the approval is exchanged for a token        |
| `oauth.<name>.user_url`      | Where the signed-in account is read from           |
| `oauth.<name>.scopes`        | Comma-separated, e.g. `openid,email`               |

All five of the required settings must be present and every endpoint must be
`https`, or the server names what is missing and refuses to start. The endpoints
are stated rather than discovered, which is why a self-hosted Forgejo, Gitea or
Keycloak is configured exactly like a commercial provider, and why a server with
no outbound connection at startup starts anyway.

**The redirect URL to register with the provider** is your canonical address
followed by `/api/auth/oauth/callback`. It is built from
`server.canonical_address` and never from a request header, so it stays right
behind a proxy — and so nobody can choose where an authorization code is
delivered.

**What happens when an email matches an existing account.** If the provider says
it has verified the address, the person is offered the chance to connect the two
and must sign in to the existing account once to accept. If the provider has not
verified it, nothing is linked and they get a new account. That second case is
not a limitation to work around: a provider where anybody can set an unverified
address to anything would otherwise be a way into any account on your server.

### Mail, and the account recovery that depends on it

Nothing is configured by default, and a server that sends no mail runs perfectly
well — what it loses is the ability to offer a password reset, which it then does
not offer rather than offering and failing.

| Setting          | Default    | What it does                                       |
| ---------------- | ---------- | -------------------------------------------------- |
| `mail.host`      | —          | The mail server. Empty is mail switched off.       |
| `mail.port`      | `587`      | The port                                           |
| `mail.username`  | —          | Where the server wants credentials                 |
| `mail.password`  | —          | Redacted wherever configuration is shown           |
| `mail.from`      | —          | Required with a host; the address mail comes from  |
| `mail.from_name` | —          | The name beside it, defaulting to the server's own |
| `mail.tls`       | `starttls` | `starttls`, `tls`, or `none`                       |
| `mail.timeout`   | `15s`      | How long a send may take                           |

SMTP rather than a provider's API, deliberately: recovery has to work against a
mail server you run yourself, and every self-hosted one speaks SMTP. A relay on
the same machine needs no credentials at all.

`mail.tls = "none"` is refused unless the host is on this machine. Without
encryption the password and the recovery link cross the network in the clear,
which is not a configuration anybody means to choose; to a relay on loopback it
is ordinary, and refusing it there would refuse the simplest working setup.

**What members get.** A link, good once and for an hour, sent only to an address
the account has confirmed. Asking discloses nothing — an address with no account
is answered exactly as one with an account, which means somebody who mistypes
theirs gets silence rather than a correction. That is the trade, and it is the
right one: the alternative is a form that tells anybody who asks which addresses
have accounts here.

Setting a new password signs out every device and does not sign the person in.
They sign in with the password they chose, which proves they know it and goes
through the second factor where the account has one.

**An account with no confirmed address still cannot be recovered**, and that is
the part to tell members about. Confirming is theirs to do, from their own
settings: they give an address, a link arrives, and using it proves the address
belongs to them. Until they do, a forgotten password is an administrator's
problem.

A confirmation link lasts a day rather than an hour — nothing is at stake in an
unclaimed one — and the account keeps whatever address it already had until the
link is used, so a mistyped address costs nothing but the message.

### Passkeys

Nothing to configure, and nothing to turn on. A server signs people in with
passkeys as soon as `server.canonical_address` is set, which it must be anyway.

That requirement is not incidental. A passkey is bound to the hostname it was
enrolled against, so one made at `chat.example.org` works only there — which is
the point, since it is what makes a passkey unphishable. It also means the
hostname must be right and must stay right: **changing your canonical address
invalidates every passkey already enrolled**, and everybody who relied on one
will need their password to get back in and enrol again. Decide the address
before people start using it.

A server with no canonical address offers none rather than binding them to a
guess, and says so in `/api/server` so clients do not draw a button that cannot
work.

Somebody may enrol several — a phone, a laptop, a hardware key — and remove any
of them, except the one that would leave them no way in at all. That last
refusal counts across every kind of credential, so a passkey beside a password
can always be removed.

### Two-step sign-in

Nothing to configure. Anybody may set up an authenticator app for their own
account, and by default nobody has to.

`auth.second_factor.policy` changes that, and it is policy rather than
configuration — it takes effect immediately, without a restart:

| Value            | Who must have one                     |
| ---------------- | ------------------------------------- |
| `optional`       | Nobody. The default.                  |
| `administrators` | Anybody who can configure the server. |
| `everybody`      | Everyone.                             |

Under either requirement, somebody without one is not locked out — they are held
short of the server and asked to set one up, and can still reach their own
account, the setup screens and sign-out. The requirement reaches sessions that
already existed, so setting it does not wait out a month of thirty-day sessions.
Somebody it applies to cannot then turn it off.

**Recovery codes are shown once.** Ten are issued when somebody finishes setting
up, each good for one use, and only their hashes are stored — so there is no
administrative way to show them again. Somebody who loses both their phone and
their codes needs you to remove the second factor for them, which is a database
operation until account recovery lands.

`administrators` is the setting most servers want. Those accounts can change the
registration policy, the limits and the branding, which makes them the ones
worth attacking, and they belong to the few people an operator can reasonably
ask to carry an app.

### Policy, which changes without a restart

Configuration is plumbing — where the database is, what port to listen on. It is
read once at startup and cannot change without one.

Policy is separate and lives in the database: who may register, how many
channels this server will hold, what it calls itself to a reader, what happens
to a filtered word. An administrator changes those through the interface or
`/api/admin/settings`, and the change takes effect immediately. Closing
registration during a flood of spam accounts at two in the morning should not
require a deployment.

Every such change is recorded in the audit log.

## TLS

**The server does not terminate TLS.** It speaks plain HTTP on its configured
address and expects something in front of it to hold the certificate. That keeps
certificate renewal, protocol versions and cipher choice in the hands of
software that does nothing else, rather than in a chat server.

So a real deployment is the compose stack with a reverse proxy in front. Caddy
is the least work, because it gets and renews certificates by itself:

```
chat.example.org {
    reverse_proxy localhost:8080
}
```

That is the entire configuration. Point the domain's A record at the machine,
run Caddy, and the certificate arrives.

With nginx or another proxy the shape is the same, and three things have to be
right:

**Forward the WebSocket.** `/api/live` is a long-lived upgraded connection.
A proxy that does not pass `Upgrade` and `Connection` headers through leaves a
client that loads, signs in, and then never sees a message arrive — which looks
like a bug in the application and is not.

**Do not buffer it, and do not time it out early.** An idle connection sends a
protocol ping every 45 seconds by default. A proxy read timeout shorter than
that closes connections constantly; clients reconnect, and nothing is lost, but
the churn is pointless. Give it comfortably more than the keepalive interval.

**Let attachments through.** Set the proxy's maximum body size to at least the
largest upload you intend to allow, or uploads fail at the proxy with an error
the application never sees and cannot explain.

Once TLS is in front, remove `TDC_SERVER_SECURE_COOKIES: "false"` from the
compose file, and bind the server's port to loopback so that the plain-HTTP port
is not reachable from anywhere else.

One thing to know about client addresses behind a proxy. Rate limiting on
sign-in and registration is per source address, and the server takes that
address from the forwarded headers a trusted proxy sets rather than believing
any caller that sends one — taking a forwarded header at face value would let
anybody forge an address and evade the limit entirely. The consequence for you
is that the proxy has to set `X-Forwarded-For`. One that does not leaves every
request looking as though it came from the proxy itself, and one noisy client's
allowance becomes everybody's.

## Backup

There are two things to back up, and they want different schedules.

**The database** holds accounts, messages, channels, permissions, the audit log,
runtime policy and the server's signing key. It is small and changes constantly.

**The attachments** are large and are written once. In the default
configuration they are in the `attachments` volume; with an S3 backend they are
wherever that store is, and backing them up is that store's business.

The compose file keeps them in separate volumes precisely so that they can be
backed up on different schedules.

### The database

```sh
docker compose exec -T database \
  pg_dump -U tdc -d tdc --format=custom --file=/tmp/tdc.dump

docker compose cp database:/tmp/tdc.dump ./tdc-$(date +%F).dump
docker compose exec -T database rm /tmp/tdc.dump
```

`--format=custom` rather than plain SQL, because it restores with `pg_restore`,
can be restored selectively, and is compressed.

A dump taken while the server is running is consistent — `pg_dump` takes a
snapshot, and nothing written during the dump appears half-way. There is no need
to stop the server.

### The attachments

```sh
docker run --rm \
  -v tdc_attachments:/data:ro \
  -v "$PWD":/backup \
  alpine tar czf /backup/tdc-attachments-$(date +%F).tar.gz -C /data .
```

The volume is named for the compose project, which the file sets to `tdc`.

### What a backup is worth

Nothing, until it has been restored. Take a dump, restore it into a throwaway
stack, sign in, and read a message. Do that once when you set the server up, and
again after any change to how the backup is taken. A backup nobody has restored
is a belief rather than a backup.

Keep at least one copy on a different machine. A snapshot of the volume on the
same disk protects against somebody deleting the wrong row and not against the
disk.

## Restore

Restoring is into an **empty** database. Restoring over a populated one produces
conflicts on every row that already exists and leaves a mixture of the two.

```sh
docker compose stop server

docker compose exec -T database dropdb -U tdc tdc
docker compose exec -T database createdb -U tdc -O tdc tdc

docker compose cp ./tdc-2026-09-17.dump database:/tmp/tdc.dump
docker compose exec -T database pg_restore -U tdc -d tdc --no-owner /tmp/tdc.dump
docker compose exec -T database rm /tmp/tdc.dump

docker compose start server
```

Stopping the server first matters. It holds a connection pool, and a `dropdb`
against a database with open connections fails.

Attachments go back the way they came:

```sh
docker run --rm \
  -v tdc_attachments:/data \
  -v "$PWD":/backup \
  alpine sh -c 'cd /data && tar xzf /backup/tdc-attachments-2026-09-17.tar.gz'
```

Restore both from the same point in time where you can. A database that knows
about an attachment whose file is missing shows a message with a file that will
not download; a file with no row is invisible and takes up disk. Neither is
fatal, and neither is pleasant to explain.

**Restore the database with the same release that made the dump, or a newer
one.** An older binary refuses to start against a schema newer than it
understands, which is the behaviour that makes rolling back safe. See
[upgrading](upgrading.md).

## Upgrading

```sh
docker compose pull && docker compose up -d
```

Migrations run at startup, before the server answers anything, so there is no
separate step. A skipped release is fine — every pending migration is applied in
order.

[The upgrade guide](upgrading.md) covers what happens when that is not the whole
story: rolling back, skipped releases, a migration that fails partway, and the
one thing about the web assets that looks like a networking fault and is not.

## Watching it run

`/api/health` answers 200 with `{"status":"ok","checks":{"database":"ok"}}`, or
503 with `"unavailable"` when it cannot serve. Point an uptime check at it; the
compose file already uses it as the container's health check. It needs no
credential and says that the server is unhealthy without saying why, because the
reason is not an unauthenticated caller's business. The reason is in the log.

Logs are JSON by default and go to standard output, so `docker compose logs -f
server` or whatever collects container logs will do. `log.format = text` is
easier to read by eye during a problem.

`/api/admin/usage` reports what is being used against what is allowed —
accounts, channels, storage. The three limits default to zero, meaning no limit,
because a server enforcing a number nobody chose stops working for reasons its
operator cannot see. If you do set them, watch this, so that a limit is visible
before it starts refusing.

The audit log records authentication, authorisation changes, moderation and
every change to runtime policy. Reading it takes its own permission, separate
from the ones that let somebody change things, so that being able to act is not
the same as being able to read who acted. `/api/admin/audit/export` takes the
whole thing as newline-delimited JSON.

## Things that catch people out

**The web assets are read once at startup.** Replacing them means restarting the
server. The application shell is held in memory and served on every navigation,
and it carries the configuration it was built with — so a server still running
against an old shell serves an application that calls the address it was built
for rather than the one it is served from. The symptom looks like a networking
fault and is not one.

**`SameSite=Lax` means one origin.** If the web app and the API end up on
different hostnames, the browser will not send the session cookie and every
request looks unauthenticated. Serve them from the same origin, which the
container already does.

**The `Secure` cookie flag defaults on.** A deployment served over plain HTTP
has to say so explicitly, which is deliberate — but it means that if you put TLS
in front and forget to remove the compose file's override, sessions keep working
and keep being sent in the clear.

**The database port in the shipped compose file is published.** Only to
`127.0.0.1`, and only for local development and tests, but remove it from a
production file anyway.

**Federation does not exist.** Servers do not exchange events. Every event is
signed and every address names its home server, so the groundwork is there, but
nothing crosses a server boundary today. A deployment is one server and the
people on it.

**One server, one process.** Multi-node deployment is not implemented — the
shared cache and the cross-process fanout that would make it work are not built.
Run one server process against one database. It will comfortably hold a
community.
