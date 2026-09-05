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

**GIF search** — not implemented

Sending a GIF works: it uploads like any other file, animates in the
conversation, and is deduplicated by content. Searching a library of them does
not, because every such library (Giphy, Tenor) requires an account and an API
key, which conflicts with the constraint above.

If it is added, it will be off by default, configured with an operator's own
key, and proxied through this server so that viewers are not tracked by the
provider.

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
