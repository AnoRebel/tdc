# TDC

Chat, project tracking, and a directory connecting developers with the people
who need them — for the Tanzania Developers Community, and for anyone who wants
to run the same thing themselves.

Free and open source. Self-hosted, always.

## What it costs to run

Nothing, and that is a design constraint rather than an aspiration.

A working server needs Docker and a machine to run it on. It needs no account
with anybody, no API key, and no paid service. Everything below marked optional
is genuinely optional: the server starts, runs and behaves correctly with every
one of them switched off, which is the default.

Where an optional feature does need a third party, it is listed here with what
it costs and what is lost by leaving it off — so the decision is made once, in
the open, rather than discovered when something stops working.

## Running it

```sh
docker compose up -d
```

That is the whole product on <http://localhost:8080>: the web application and
the API on one port, with Postgres behind them. The database schema, the
attachment store and the server's signing key are created on first start, so
there is nothing to prepare beforehand.

One origin is deliberate rather than convenient. The session cookie is HttpOnly
and SameSite=Lax; served from a different host, a browser treats it as
third-party and declines to send it back. It also means no CORS preflight on
every request, and one port to expose.

For development, the web application runs from its own server with hot
reloading:

```sh
bun install
bun run --cwd apps/web dev
```

That serves on port 3000 and reaches the API on 8080 — two origins, which is
what the `TDC_SERVER_ALLOWED_ORIGINS` line in `compose.yaml` allows for.

Opening the app on a server with no accounts leads to first-run setup, which
creates the administrator. That route closes as soon as an account exists, so
it cannot be used to grant administration to somebody who arrives later.

## On the desktop

A native application for Linux, macOS and Windows, wrapping the same interface
in a window and adding what a browser cannot do: notifications that survive the
window being closed, credentials in the operating system's keychain, and a tray
icon carrying the unread count.

It is optional. The web application is installable to a home screen and works
offline on its own, and on a phone or tablet that is the one to use.

Building it is documented in [apps/desktop](apps/desktop/README.md).

## Upgrading

```sh
docker compose pull && docker compose up -d
```

Migrations run on start, so there is no separate step. A version that changes
the schema is applied before the server begins answering.

Replacing the web assets means restarting the server. The application shell is
read once at start and served from memory on every navigation, and it carries
the configuration the application was built with — so a server still running
against an old shell serves an application that calls the address it was built
for rather than the one it is served from. The symptom looks like a networking
fault and is not one.

## Configuration

Settings come from a file or from the environment, and the environment wins.
An environment variable is the setting's name in capitals with dots as
underscores, prefixed with `TDC_`: `storage.backend` is `TDC_STORAGE_BACKEND`.

### Storage

| Setting                 | Default              | What it does                     |
| ----------------------- | -------------------- | -------------------------------- |
| `storage.backend`       | `local`              | `local`, `s3-aws` or `s3-minio`. |
| `storage.directory`     | `./data/attachments` | Where files live under `local`.  |
| `storage.s3.endpoint`   | —                    | Required by both S3 backends.    |
| `storage.s3.bucket`     | —                    | Required by both S3 backends.    |
| `storage.s3.access_key` | —                    | Required by both S3 backends.    |
| `storage.s3.secret_key` | —                    | Required by both S3 backends.    |

`local` keeps attachments in a directory, which is why a small deployment needs
no object store at all. The two S3 backends speak to the same stores and behave
identically — the choice between them is preference, and changing it is one
word.

### Settings an operator changes while it runs

These are policy rather than plumbing, so they live in the database and change
without a restart — closing registration during a flood of spam accounts should
not require a deployment. An administrator sets them through
`/api/admin/settings`, and every change is recorded in the audit log.

| Setting                    | Default | What it does                                                       |
| -------------------------- | ------- | ------------------------------------------------------------------ |
| `registration.policy`      | `open`  | `open`, `invitation`, `closed`, or `domain`.                       |
| `registration.domains`     | —       | Email domains admitted under the `domain` policy, comma separated. |
| `limits.max_users`         | `0`     | Maximum accounts, or 0 for no limit.                               |
| `limits.max_channels`      | `0`     | Maximum channels, or 0 for no limit.                               |
| `limits.max_storage_bytes` | `0`     | Maximum attachment storage, or 0 for no limit.                     |

A policy change governs creating an account, never an existing one: somebody
who registered while it was open keeps their account when it closes.

Zero means no limit, and that is the default for all three — a server enforcing
a number nobody chose would stop working for reasons its operator cannot see.
`/api/admin/usage` reports what is being used against what is allowed, so a
limit is visible before it starts refusing.

Under the `invitation` policy somebody registers with a link or a code an
existing member issued. Channels have their own invitations too, for admitting
somebody to one conversation rather than to the server.

An invitation may expire, carry a use limit, and be bound to one email address
so that forwarding it does not work. Revoking one takes effect immediately —
somebody already holding the link cannot be un-sent it, so the only withdrawal
is the server refusing to honour it.

Every refusal is the same refusal. Expired, exhausted, revoked, bound to
somebody else and never-existent all answer identically, because a response
that distinguished them would tell somebody guessing codes when they had
guessed right. Codes are rate limited far more strictly than links for the same
reason: a code is short enough to type, and therefore short enough to guess at.

### Optional external services

None of these are needed, and all but link previews are off by default. Each
says what it costs and what is lost.

**Malware scanning** — `attachments.scanner_address`

Off by default. Set it to a `clamd` host and port and every upload is scanned
before it is stored. ClamAV is free and self-hosted, so this costs a container
rather than money.

Without it, uploads are not scanned for malware. Everything else that protects
readers still applies: types are detected from content rather than believed,
executables are refused, and files are served inert.

`attachments.scanner_fail_open` decides what happens when the scanner cannot be
reached. It is off — uploads are refused rather than accepted unscanned —
because a scanner that is configured but unreachable is a scanner somebody is
relying on.

**Media sidecar** — `attachments.media_sidecar_address`

Off by default. Preview generation runs in the server process, using only the
Go standard library, so there is no image library to keep patched. An operator
who wants that work isolated in its own container can point this at one.

Without it, previews are generated in-process. This is the ordinary case.

**Link previews** — `attachments.link_previews`

On by default, and the one optional feature that is. A link posted in a
conversation is fetched by the server and shown with its title and description.

It needs no third party — the server fetches the page itself — which is why it
is on. It does mean the server makes a request to a URL somebody else chose,
so the address is checked at connect time rather than when it is resolved: a
name that resolves to a private address is refused even if it resolved
differently a moment earlier. An operator who would rather the server made no
such request sets this to `false`.

**GIF search** — `gifs.provider` and `gifs.api_key`

Off by default, and the only option here that costs an account. Set the
provider to `tenor` or `giphy` and supply your own key with them, and a GIF tab
appears in the emoji picker.

| Setting               | Default   | What it does                                                           |
| --------------------- | --------- | ---------------------------------------------------------------------- |
| `gifs.provider`       | —         | `tenor`, `giphy`, or empty for no search.                              |
| `gifs.api_key`        | —         | Your own key with that provider.                                       |
| `gifs.content_filter` | strictest | Tenor: `off`, `low`, `medium`, `high`. Giphy: `g`, `pg`, `pg-13`, `r`. |
| `gifs.timeout`        | `5s`      | Bounds a search.                                                       |

Setting one without the other is refused at startup rather than silently
leaving the feature off, because half a configuration is somebody who meant to
switch it on.

Searches are proxied through this server. The key stays here, and the provider
sees one server rather than an address and a request per keystroke for
everybody using it. A chosen GIF is uploaded as an ordinary attachment, so it
is stored here, deduplicated by content, and still readable when the provider
takes it down.

Without it, sending a GIF still works — it uploads like any other file and
animates in the conversation. Only searching a library from inside the app is
missing.

### Link previews of this server

A public channel's address, and the server's own, carry the metadata that link
previews read — so posting one into another chat application shows what it is
rather than a blank box with a URL.

Nothing private does. A private channel's link produces no metadata at all, not
even a line saying it is private, because a preview is generated for anybody
holding the link including somebody with no account here: naming one would
confirm it exists.

An invitation link previews as an invitation to the server and nothing more. It
never names the channel — an invitation posted into a group chat is previewed
for everybody in it, not only for the person being invited — and it previews
identically whether it is valid, revoked or invented, so nobody can test
invitations by pasting them somewhere that renders previews.

### Federation

Off. Servers do not yet exchange events. The groundwork is in place — every
event is signed, and addresses name their home server — but nothing crosses a
server boundary today.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Changes arrive as pull requests against
`dev`; `dev` flows to `beta` and then to `main`.

AI-assisted contributions are welcome, and must say so. State in the pull
request that AI was used and give the reasoning behind the change — what
problem it solves and why this is the right way to solve it. A patch nobody can
explain is not reviewable, whoever or whatever wrote it.

## Licence

See [LICENSE](LICENSE). Fork it and change it as the licence allows.
