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

### Running more than one node

One process is the ordinary deployment and needs nothing here. Two or more
behind a load balancer need two settings, and they go together:

| Setting             | What it does                                                                                  |
| ------------------- | --------------------------------------------------------------------------------------------- |
| `server.multi_node` | Share events between nodes, so a message sent through one reaches people connected to another |
| `cache.address`     | Share rate-limit counters, so a limit is one limit rather than one per node                   |

**Setting one without the other is a mistake the server warns about.** Events
without a shared cache means every rate limit is enforced once per node — five
sign-in attempts becomes five per node, which nobody notices until it is being
exploited. A shared cache without events means the limits are right and half
your community sees a conversation that appears to have stopped.

Events travel over Postgres's own `LISTEN`/`NOTIFY` rather than a queue you
install: you are already running Postgres. The notification carries the event's
identity, and each node reads the event from the database — so nothing is lost
to the eight-kilobyte limit on a notification, however long the message.

**An application's private answer to a press crosses nodes without being
stored.** It is meant for one person, so it never enters the log; it travels
in the notification itself, which Postgres holds in memory only until every
node has read it — no table, no write-ahead log, no backup. A node with nobody
to give it to drops it unread. Only when presence says the person is connected
to another node is it sent at all, and the program waits — a second at most —
for those nodes to say they delivered it, so the count it is given is true.

Scheduled messages are sent by whichever node gets to them first. Every node
looks; each claims what it sends in the same statement that finds it, so
nothing is sent twice.

What that does not give you is durability. A node that is restarting misses the
announcements sent while it was down. That is fine here and would not be
elsewhere: the event is in the log regardless, so a client reconnecting to that
node receives everything it missed, which is the same path a client on a bad
connection already takes.

The cache is Valkey. Redis works too — nothing here uses anything the two do not
share — but Redis is no longer open source under a licence this project can
depend on, and Valkey is the fork that stayed.

| Setting          | Default | What it does                                 |
| ---------------- | ------- | -------------------------------------------- |
| `cache.address`  | —       | `host:port`. Empty is the in-process cache.  |
| `cache.username` | —       | Where the cache wants credentials            |
| `cache.password` | —       | Redacted wherever configuration is shown     |
| `cache.database` | `0`     | For sharing an instance between applications |
| `cache.tls`      | `false` | For reaching one across a network            |

A server configured with a cache checks it can reach it at startup, rather than
discovering it cannot when somebody tries to sign in.

### Roles

Two exist without you doing anything. `Administrator` carries
`server.administer`, which implies every other permission, and goes to the
account created during first-run setup. `Member` goes to every account as it
registers, and grants creating a channel and inviting somebody to one.

`Member` is an ordinary role. A server where only administrators make channels
is one withdrawal away:

```
PUT /api/admin/roles/{id}/permissions
{ "permission": "channel.create", "granted": false }
```

Both are built-in, meaning they cannot be deleted — the accounts holding them
would be left holding nothing — but their permissions are yours to change. Roles
beyond them are yours to make, under `/api/admin/roles`.

The server refuses two changes however they are asked for: one that would leave
it with no administrator at all, and one that would remove the authority of the
person making it. The second can be overridden — somebody stepping down
deliberately is a real thing — by repeating the request with `confirmed`.

Thirty-seven of the thirty-nine catalogued permissions are enforced — every one
that projects and the directory added among them — and the two left,
`call.start` and `call.join`, are names waiting for calls. `channel.read` and
`channel.post` are checked per channel, on top of membership, bans and posting
restrictions, and Member holds both everywhere until you say otherwise.

Looking after a channel is four permissions, each grantable for one channel so
a channel can have keepers with no say anywhere else: `channel.rename` (its
name, its topic, and making it private), `channel.archive` (archiving and
restoring), `channel.member.remove` (taking somebody out, which is not a ban),
and `message.edit.any` (changing somebody else's message, shown as the
moderator's edit). Making a channel **public** needs `channel.rename` for the
whole server, since it opens the channel's entire history to everybody. Every
one of these is audited — `channel.renamed`, `channel.visibility.changed` with
both values, `channel.archived`, `channel.restored`, `channel.member.removed`
and `message.edited_by_moderator`. None applies to a direct conversation, a
project's discussions, or a channel another server holds. `user.read.all`
opens the list of every account at `/api/admin/users` (as `user.suspend`
already did), paged with `limit` and `offset`; it shows no email addresses,
and a hidden directory profile stays behind `directory.profile.manage`.

Member is a baseline beneath every other role: whatever another role says about
a permission in the same place replaces what Member says there. That is what
makes an announcement channel two calls rather than a restriction per person —
withdraw `channel.post` from Member in that channel, and grant it there on a
role held by whoever announces:

```sh
curl -X PUT "$TDC/api/admin/roles/$MEMBER/permissions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"permission\": \"channel.post\", \"channel\": \"$CHANNEL\", \"granted\": false}"
curl -X PUT "$TDC/api/admin/roles/$ANNOUNCER/permissions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"permission\": \"channel.post\", \"channel\": \"$CHANNEL\", \"granted\": true}"
```

The grant has to be for that channel: a role granting `channel.post` everywhere
does not reopen a channel where Member is denied it. Administrators are not held
to Member at all, so somebody promoted from the membership posts there as the
person who set the server up does. Between any other two roles a denial still
wins.

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

### Notifications: bursts, and summaries by email

Two pieces of policy decide how much notifications interrupt people. Both
change while the server runs, through `/api/admin/settings` or the settings
page, and need no restart:

| Setting                                  | Default | What it does                                                                                                                                             |
| ---------------------------------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `notifications.batch_seconds`            | `60`    | After a device is told about a conversation, further messages there within this many seconds are not pushed again. `0` pushes every one; at most `3600`. |
| `notifications.digest_max_conversations` | `20`    | The most conversations one emailed summary lists; the rest are counted. At least `1`.                                                                    |

**Batching** applies to pushes only. Every message is still raised as a
notification, still counted and still listed — the list already shows one entry
per conversation — so what the window changes is how often somebody's phone
lights up during a busy exchange. Reading the conversation ends the window
early, so the next message after that is news again.

**Summaries** are each member's own choice: a daily or weekly email, at an hour
in their own time zone, listing the conversations where something went unread
and how much. They replace pushes for whoever chooses them. A summary counts and
never quotes — no message text goes into the mail — and it is checked at the
moment it is sent, so a channel somebody has left, been banned from, had
`channel.read` withdrawn in, or muted is left out entirely. A status that
suppresses notifications holds the summary until it changes. Summaries go only
to a confirmed address, like recovery.

They need mail. A server with no `mail.host` does not offer them — the control
is not shown, `notification-digests` is absent from `/api/server`, and the
routes answer `404` — and anybody who chose one before mail was switched off
goes back to ordinary pushes rather than being left with neither. With mail
configured they are on by default; the `notification-digests` feature toggle
withdraws them if you want mail to carry recovery and nothing else:

```sh
curl -X PUT https://tdc.example/api/admin/features/notification-digests \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled": false}'
```

Every node looks for due summaries once a minute. Servers sharing a database
share the work: each summary is claimed in the statement that finds it, so
nobody is mailed twice however many nodes run. The zone database is built into
the server, so "08:00 in Africa/Dar_es_Salaam" is right whatever zone the host
itself is set to.

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

Where you have configured mail, somebody whose phone is out of reach can have a
code sent to their address instead — but only if that address has been
confirmed, and only as a fallback. It is the weakest of the three factors,
because it is worth exactly as much as the mailbox: somebody who has taken over
an inbox has taken over this. It exists because the alternative for somebody
with a lost phone is writing to you.

A passkey satisfies the requirement. Somebody who has enrolled one already
carries the thing a second factor is for, so they are asked for it as the second
step rather than being told to set up an authenticator app as well — which
matters if you require two-step sign-in, because the requirement then costs a
passkey holder nothing.

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

### Your privacy policy and terms

The repository carries a [privacy policy](privacy-policy.md) and
[terms of use](terms-of-use.md), and both are templates: what your server keeps,
for how long, and who answers for it are yours to say. Adapt them, publish them
somewhere you already publish things — your website, a wiki, a repository — and
tell the server where:

| Setting                    | What it is                    |
| -------------------------- | ----------------------------- |
| `legal.privacy_policy_url` | A link to your privacy policy |
| `legal.terms_url`          | A link to your terms of use   |

Both are policy, so they are set from the server settings page or
`PUT /api/admin/settings/legal.terms_url`, and take effect immediately. Every
client links to them from its help centre, and `GET /api/server` carries them
under `legal` so a client can show them before anybody has signed in.

Each must be an `http` or `https` link. Leave one empty and the clients show
nothing for it — never the templates, because a template is not a promise you
have made.

### Talking to other servers

Federation is off until you turn it on, and a server does not need it: everything
in this guide describes a complete TDC server that talks to nobody else.

| Setting                              | Default | What it does                                               |
| ------------------------------------ | ------- | ---------------------------------------------------------- |
| `federation.enabled`                 | `false` | Whether this server exchanges anything with other servers. |
| `federation.allow_private_addresses` | `false` | Whether peers on private network addresses may be reached. |

Two things are worth knowing before you enable it.

**Your canonical address has to be right.** A peer decides whether to trust this
server by fetching `https://<your domain>/.well-known/tdc-federation` and
checking that the address inside names the same domain it fetched from. If
`server.canonical_address` says something else — a stale value, an internal
hostname, an IP — every peer will correctly refuse you as an impostor. The
symptom is peers rejecting you while the server itself looks healthy.

**Turning it off does not hide you.** A disabled server still answers at the
well-known path, saying `"enabled": false`. That is deliberate: a peer that gets
a refused connection cannot tell a server that does not federate from one that is
down, and an operator debugging the difference deserves an answer. Everything
else federated refuses outright.

**Keep the clock right.** Federated requests are signed with a thirty-second
validity window, and a receiving server refuses anything already expired or
more than five seconds in the future. A server whose clock has drifted will
have every federated request refused, and the refusal reads as a signature
problem rather than the clock problem it is. Run NTP; the container images
already do.

Once federation is on, an administrator can check a peer before trusting
anything to it:

```sh
curl -X POST https://your.server/api/admin/federation/ping \
  -H 'Content-Type: application/json' \
  -b "$YOUR_SESSION" \
  -d '{"peer":"https://their.server"}'
```

It answers `{"reached":true}` or reports why not — an unreachable address, an
unpublished key, an expired signature from a bad clock. One request, before
rooms and messages depend on it.

### Choosing who to federate with

Three settings, and a block beats everything.

| Setting                      | Default | What it does                                                                                                                                 |
| ---------------------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `federation.mode`            | `open`  | `open` deals with any server that proves who it is, `allowlist` only with servers you list, `blocklist` with anyone except servers you list. |
| `federation.allowed_servers` | —       | Comma-separated, used under `allowlist`.                                                                                                     |
| `federation.blocked_servers` | —       | Comma-separated, honoured under **every** mode.                                                                                              |

The blocklist is consulted whatever the mode is. Blocking a server is you
saying "not this one", and it would be a trap for that to quietly stop meaning
anything because you later switched to `open`.

Blocking stops traffic **both ways**, immediately, with no restart: requests
from that server are refused before their signature is even examined, and
nothing is sent to them — the address is not even looked up. One list, read
from both directions, so the two cannot drift apart.

Servers are identified by **hostname**, not hostname and port. Blocking
`peer.example.org` blocks it on every port, because a server on `:8443` is the
same server as on `:443`.

Every change here is in the audit log under its own name —
`server.federation.enablement_changed`, `.mode_changed`, `.peers_changed` — so
"when did we start federating with them, and who decided that" is a question
the log answers directly.

### If a signing key is compromised

Rotate it. That is the only mechanism, and you should know what it does not do.

Peers cache your published keys for up to an hour. A peer that fetched your
document before you rotated **keeps accepting signatures from the old key until
its cache expires** — there is no revocation message, and sending one that
peers were free to ignore would look like revocation without being it.

So: rotate at once, then contact your peers' operators and ask them to refresh.
That is what shortens the window, and nothing else does.

The retired key stays published deliberately, so that everything it signed
before the compromise keeps verifying. "Retired" means "still verifies old
events" — it does not mean "safe".

### What federation costs

Federation is not free, and the cost is worth understanding before you enable
it rather than after.

**A room belongs to one server.** Whichever server a room was created on is its
home server: it assigns the order of messages, and it decides membership. Other
servers hold a replica.

**If a room's home server goes down, remote participants lose that room** until
it comes back. Not degraded — gone. Their own server is running perfectly and
still cannot serve them that room, because the alternative is for it to start
making decisions the home server is supposed to make, and two servers deciding
a room's state independently is precisely the failure federation must not have.

The reverse is better: if a _participating_ server goes down, everyone else
carries on. Only that server's own users are affected, and they get the room
back when it returns.

So the practical question is not "should we federate" but "which rooms should
this server host". A room your community depends on daily is better hosted
here, where your uptime is your own. A room you join on somebody else's server
is a room whose availability is theirs.

**The two failures are not the same, and it is worth being precise:**

| What goes down             | Who loses what                                                                                                                                                          |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A **participating** server | Only that server's own users, and only until it returns. Everyone else carries on; the room is unaffected. Its users catch up on everything they missed in one request. |
| A room's **home** server   | Every remote participant loses that room entirely until it comes back. Their own servers are healthy and still cannot serve it.                                         |

The asymmetry is the design, not a defect. A replica could keep the room usable
during a home-server outage only by deciding message order itself — and two
servers ordering the same room independently produces two different
conversations that can never be reconciled. Losing a room for an afternoon is
recoverable. Two divergent histories are not.

**What this means for you as an operator.** If you host rooms that other
communities join, your downtime is their downtime, and they will notice.
Deciding to host for others is taking on that obligation; deciding to join
rooms elsewhere is accepting it from somebody else. Neither is wrong — but a
community whose main channels live on a server run by somebody they have never
met should know that is the arrangement.

The other standing cost is that **you inherit some of your peers' problems**:
their spam reaches your users in shared rooms, and their moderation decisions
are theirs to make. The blocklist is the remedy, and it is a blunt one.

### Moderating what arrives from elsewhere

Your moderation applies to federated content exactly as to local content —
removal, filters, reports — and it applies **only here**.

Removing a federated message empties it on this server. No request is sent to
the server it came from, and none would help: it is not obliged to agree, and
asking would either be ignored or become a way for any server to delete from
any other. So the origin keeps its copy, every other participating server keeps
theirs, and what your moderator decided is what this server carries.

The audit log records it, and for a federated message that record is the only
account of what happened anywhere — the origin has no idea it occurred.

The same holds in reverse. A moderator on a peer can remove your community's
message from their copy without telling you, which is worth knowing before
federating with a server whose moderation you have not thought about.

The [federation protocol](federation-protocol.md) describes what is published
and how a peer checks it.

### Deliveries to other people's endpoints

Webhooks and applications make this server send HTTP requests to addresses
somebody else chose. Your network has to let it reach them, and you decide one
thing about where it may not.

**Private addresses are refused by default.** An endpoint on loopback, on
`10.0.0.0/8`, `172.16.0.0/12` or `192.168.0.0/16`, on a link-local or
carrier-grade NAT address, or anywhere else reserved, is refused when somebody
names it and again at the moment of connection — so a name that resolves to a
public address when it is checked and a private one when it is used gains
nothing. This matters most where anybody may set up a webhook: without it, a
webhook is a way to make your server talk to your database, your metadata
service, or anything else behind your firewall.

If your receivers genuinely live beside the server — a build system on the same
private network, say — turn it off deliberately:

```sh
curl -X PUT https://chat.example.org/api/admin/settings/integrations.allow_private_addresses \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"true"}'
```

Redirects are never followed either way. Each attempt is bounded by
`integrations.delivery_timeout_seconds` (default 5).

The keys receivers verify deliveries with are the event log's own signing keys,
published at `/.well-known/tdc-delivery-keys`, retired ones included. Rotating
the signing key — see [If a signing key is compromised](#if-a-signing-key-is-compromised)
— rotates them too, with the same caveat: a receiver that cached the list keeps
trusting the old key until it refetches. Receivers following the integration
guide refetch when a delivery names a key they do not hold, so new deliveries
reach them at once; telling their owners to refetch is what closes the window
for the old one.

### The directory

The developer and company directory is **off by default**. A server that only
wants chat never has to think about it: every directory route answers `404`,
nothing is listed, and the sidebar shows no link to it.

Turning it on is a feature toggle, changed while the server runs:

```sh
curl -X PUT https://tdc.example/api/admin/features/directory \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled": true}'
```

or the **What this server offers** list on the server settings page of the web
client (Settings → Administration). It takes effect
on the next request, needs no restart and no migration, and turning it off
again hides everything without deleting any of it — profiles are there as they
were when it comes back on.

Nobody appears in the directory because it was switched on; each person decides
for themselves by creating a profile. Whether people who are not signed in can
read profiles marked _Anyone_ follows the server's own discoverability
(`server.visibility`): only a public server shows them to strangers.

Moderating the directory takes **`directory.profile.manage`**, a server-scoped
permission to grant to whichever role your moderators hold. See
[the directory](directory.md#moderation) for what it allows and what it records.

**The skill vocabulary** starts with about fifty common skills and their usual
other spellings. It grows through the review queue: grant
**`directory.skills.manage`** to whoever should decide what counts as a skill,
and anything they propose goes straight in, while everybody else's proposals
wait for them. Seeding a longer list is a matter of that person proposing each
one — `POST /api/directory/skills` with a `name` — which is scriptable. With a
reviewer's token and one skill per line in `skills.txt`:

```sh
while IFS= read -r skill; do
  curl -s -X POST https://tdc.example/api/directory/skills \
    -H "Authorization: Bearer $REVIEWER_TOKEN" -H 'Content-Type: application/json' \
    -d "$(jq -n --arg name "$skill" '{name: $name}')"
done < skills.txt
```

A name the vocabulary already knows — under that spelling or another — is
answered with the existing skill rather than added twice, so running it again
is harmless.

**Verifying claims** takes **`directory.verify`**. It is separate from the other
two because it is a different judgement — whether evidence for a claim is
credible — and a community may want it held by different people.

**How fast the directory may be read** is policy, changed like any other
setting:

| Setting                                | Default | What it limits                                                |
| -------------------------------------- | ------- | ------------------------------------------------------------- |
| `directory.reads_per_minute`           | 30      | searches and profile views per signed-in person per minute    |
| `directory.anonymous_reads_per_minute` | 10      | the same, per address, for people browsing without signing in |
| `directory.enquiries_per_hour`         | 5       | enquiries one person may send in an hour                      |

Each must be at least one; an operator who wants the directory unreadable
turns it off. A page of results is always twenty at most, whatever these say.
They take effect on the next request:

```sh
curl -X PUT https://tdc.example/api/admin/settings/directory.reads_per_minute \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"value": "60"}'
```

**Upgrading and switching on are separate steps.** The directory's tables are
created by the ordinary migrations when the server is upgraded, whether or not
the directory is ever switched on, so switching it on later — or off and on
again — touches no schema and needs no maintenance window. Existing accounts
need nothing done to them: none of them has a profile until its owner makes
one.

**Switching it off** removes it from people's view straight away: every
directory route answers `404`, the server stops listing `directory` among what
it offers, the sidebar link goes at the next page load, and a bookmarked
directory page lands on the start page instead. Nothing is deleted. Profiles,
companies, verifications, enquiries, engagements, proposals and agreements are
all kept and reappear as they were when it is switched back on. The private
conversation each enquiry opened is an ordinary direct conversation and stays
available to the two people in it, like any other.

### Projects

Projects need nothing configured and nothing extra running: they are tables in
the same database, and their history is the same event log messages use. Two
things about how they are stored are worth knowing when you look at the
database or its backups.

- **Every project and every issue has a room behind it.** A project's history is
  a room of kind `project` — its event stream, which nobody is a member of and
  which no room endpoint serves — and each issue's discussion is a room of kind
  `issue-discussion`. Neither kind appears among anybody's conversations, and
  neither counts towards `limits.max_channels`. A backup of the database is a
  backup of all of it.
- **Upgrading grants `project.create` to the `Member` role**, the same way a new
  server's `Member` role is made with it, so ordinary accounts can start
  projects as they can make channels. Withdraw it like any other permission if
  you want projects started only by some people.
- **The server ends cycles by itself.** Once a minute it looks for cycles whose
  last day is over in their project's calendar and deals with each under the
  project's policy, moving only which cycle unfinished issues are in. Servers
  sharing a database share the work: each cycle is locked while it is dealt
  with, and another server skips it. The zone database is built into the
  server, so a container without `/usr/share/zoneinfo` still knows every
  project's calendar.

The audit log records the administrative acts around projects — one started,
its visibility changed, somebody added to or removed from it, a role granted a
permission in one — as `project.*` actions. What happens to issues is not
copied there: it is each project's own history, in the event log.

[Projects and issues](projects.md) covers the rest.

### Calls

Voice and video calls are **off by default**. Turn them on while the server
runs:

```sh
curl -X PUT https://tdc.example/api/admin/features/calls \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled": true}'
```

**What the server does, and does not.** It tells the people in a call about each
other and passes on the few kilobytes each pair needs to find a way to connect.
The audio and video go **directly between the people in the call**, encrypted by
WebRTC itself (DTLS-SRTP) — they never pass through the TDC server. So calling
costs the server almost nothing: a few small messages per person joining, and a
sweep every fifteen seconds that takes people whose connection dropped out of
their calls. Nothing a call carries is stored, and the server has no way to
record one.

**What it costs the people in it.** Every participant sends their own stream to
every other participant (a _mesh_), so upload grows with the size of the call:

| People in the call | Each person sends (voice) | Each person sends (video, ~720p) | Each person receives |
| ------------------ | ------------------------- | -------------------------------- | -------------------- |
| 2                  | ~50 kbit/s                | ~1–1.5 Mbit/s                    | the same             |
| 3                  | ~100 kbit/s               | ~2–3 Mbit/s                      | the same             |
| 4                  | ~150 kbit/s               | ~3–4.5 Mbit/s                    | the same             |
| 6                  | ~250 kbit/s               | ~5–7.5 Mbit/s                    | the same             |

Browsers lower video quality by themselves when a connection cannot keep up,
and encoding one stream per other person also costs CPU — comfortable for four
people on a laptop, hard work for a phone with video past three. That is why
`calls.max_participants` defaults to **4**. Raise it for voice-heavy
communities with good connections; past six, a mesh stops being a good idea for
anybody, and calls that size wait for the forwarding unit still to come.

| Setting                  | Default | What it does                                                                     |
| ------------------------ | ------- | -------------------------------------------------------------------------------- |
| `calls.max_participants` | `4`     | The most people one call may hold; the next is told the call is full             |
| `calls.stun_urls`        | —       | STUN servers, comma-separated (`stun:host:3478`), for finding a direct path      |
| `calls.turn_urls`        | —       | TURN relays, comma-separated (`turn:host:3478?transport=udp`, `turns:host:5349`) |

And one piece of configuration, a secret rather than a setting:

| Configuration       | Environment             | What it does                                                    |
| ------------------- | ----------------------- | --------------------------------------------------------------- |
| `calls.turn_secret` | `TDC_CALLS_TURN_SECRET` | Shared with the relay; without it no relay is offered to anyone |

**Nothing is configured to a third party by default.** No public STUN server is
set, because one learns who is calling whom. With nothing set, calls connect
between people who can reach each other directly — on one network, or on
ordinary home connections — and fail between people behind stricter networks
(offices, mobile carriers, some routers). For a community whose members call
from phones and offices, run the relay.

**The relay** is [coturn](https://github.com/coturn/coturn) (BSD licence),
included in the compose file behind the `calls` profile:

```sh
export TDC_CALLS_TURN_SECRET="$(openssl rand -hex 32)"   # the server reads the same variable
docker compose --profile calls up -d
curl -X PUT https://tdc.example/api/admin/settings/calls.turn_urls \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"value": "turn:tdc.example:3478?transport=udp,turn:tdc.example:3478?transport=tcp"}'
```

It also answers STUN, so `calls.stun_urls` can be `stun:tdc.example:3478`.

- **Ports to open:** `3478` UDP and TCP, and `49160–49200` UDP for relayed media.
  Each relayed participant in a call holds a port from that range, so the range
  is how many relayed participants you can carry at once; widen `--min-port` /
  `--max-port` together with the firewall for more.
- **Bandwidth:** the relay carries a relayed participant's streams both ways,
  so plan for the table above, per relayed person, on the relay's host — a
  four-person video call with everybody relayed is roughly 12–18 Mbit/s each way.
  In practice most people connect directly and only a few calls are relayed.
- **CPU and memory:** small — it forwards encrypted packets it cannot read.
  Half a CPU core and 256 MB carry dozens of relayed participants.
- **Credentials** are minted by the TDC server per person and expire after four
  hours, so the relay is not an open proxy; it refuses to start without a
  secret, and never relays into private networks.

**Clients.** Current Chrome, Edge, Firefox and Safari all place calls, on
desktops and phones. The desktop application uses its platform's web view:
Windows and macOS support calls; on Linux they depend on WebKitGTK being built
with WebRTC and GStreamer's WebRTC plugins being installed. Picture-in-picture
of the whole call needs a Chromium-based browser; others put the main video
there instead.

**Ringing.** People can ring each other into calls; a ring reaches every
device of the person rung, by the live connection, and by web push where the
server has push configured, so a browser in the background still rings.
Ringing is limited to ten rings per person every five minutes. Each person
decides who may ring them; whether people on connected servers may ring anybody
here is the server's decision first.

**Not yet:** links to a call, screen sharing and recording;
calls in conversations shared with other servers; and calls of more than a few
people through a forwarding unit.

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

### What it can carry

Measured rather than estimated, by a check that runs in continuous integration
and prints its figures into the build log — so these numbers are re-taken on
every change rather than being a claim made once.

Fanout is the measurement that matters, because one message into a channel
everybody is watching becomes one delivery per connected member, and that
multiplication is what decides whether a busy channel stays usable. On a
development machine, 250 connections all watching one channel and 40 messages
posted into it — 10,000 deliveries — arrive with a median under 200µs and a
slowest delivery under a millisecond.

Two things are asserted rather than reported. Every entitled connection
receives every message, because a fanout that got faster by dropping
deliveries would look like an improvement. And a client that stops reading
cannot stall the channel: its buffer fills, it is dropped, and everybody else
carries on. One slow phone must not make a channel slow for the room.

These are not a guarantee about your hardware. They are a baseline: if the
figure in your own logs is far from this, something is different, and the
difference is worth finding before people notice it.

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

**Federation is off until you turn it on, room by room.** A server exchanges
nothing with any other until federation is enabled, a peer is trusted, and a
room is federated — see [Choosing who to federate with](#choosing-who-to-federate-with). A room that is not federated
never leaves the server, whatever else is configured.

**More than one node is optional, and one is enough.** One server process
against one database will comfortably hold a community. Several are supported
— see [Running more than one node](#running-more-than-one-node).
