# Security policy

TDC is software that communities run for themselves, so a vulnerability in it is
a vulnerability on every server that runs it — servers whose operators may never
read this repository. Reporting one privately, and giving time for a fix to
reach them before it is public, is what protects the people on those servers.

## Reporting a vulnerability

**Please do not open a public issue, discussion or pull request for a
vulnerability.** Anything posted there is readable by everybody at once,
including people who would use it before a fix exists.

Report it privately through GitHub instead:

1. Go to the repository's [Security tab](https://github.com/AnoRebel/tdc/security).
2. Choose **Report a vulnerability**, or go straight to
   [the private report form](https://github.com/AnoRebel/tdc/security/advisories/new).

Only the maintainers can read what you send, and the conversation, the fix and
eventually the published advisory all happen in that one place. You need a
GitHub account; nothing else.

A useful report says:

- **what is affected** — the component (server, web application, desktop client,
  federation) and the version or commit you tested;
- **what an attacker can do** — read someone else's messages, act as another
  account, reach a channel they were removed from, run code on the server;
- **how to reproduce it** — the requests, the configuration, the steps. A
  script or a failing test is the most useful thing you can send;
- **what you think the fix is**, if you have an idea. Not required.

If you are unsure whether something is a vulnerability, report it privately
anyway. A report that turns out to be ordinary behaviour costs a short reply; a
real vulnerability discussed in public costs everybody running a server.

## What happens next

These are what the project aims for. It is maintained by volunteers, so they are
intentions rather than contractual deadlines, and you will be told if one is
going to slip.

- **Acknowledgement within seven days**, saying the report has been read.
- **An assessment** — whether it is a vulnerability, how severe, and which
  versions it affects — usually within two weeks of acknowledgement. If it is
  not judged a vulnerability you will be told why, and you are free to disagree
  and say so.
- **A fix**, developed in a private fork attached to the advisory, with a test
  that fails without it. You are welcome to review it before it is released.
- **A release** carrying the fix, and the advisory published alongside it, with
  a CVE identifier requested through GitHub where the issue warrants one.

Security fixes take priority over every other piece of work in the project.

## Coordinated disclosure

Please give the project a chance to fix a problem and ship the fix before
describing it publicly. The default is **90 days from your report**, or the
release of a fix if that comes sooner — after which you may publish whatever
you like, whether or not a fix exists.

The window may be agreed shorter or longer between you and the maintainers. It
should be shorter if the vulnerability is already being exploited or already
public, and may be longer if the fix needs a change to the federation protocol,
which other servers have to adopt as well.

The advisory credits you by name, handle or not at all, as you choose. Nothing
about your report is published without your agreement before the advisory is.

## Supported versions

TDC has not reached 1.0. Until it does, **only the latest release is
supported**: a fix goes into the next release, and earlier releases are not
patched separately. Upgrading across several releases at once is supported and
needs no manual steps (see [Upgrading](docs/upgrading.md)), so moving to the
release with the fix is the remedy for any older version.

| Version          | Supported                                               |
| ---------------- | ------------------------------------------------------- |
| Latest release   | Yes                                                     |
| Earlier releases | No — upgrade to the latest                              |
| `dev` and `beta` | Fixes land here first; reports against them are welcome |

This will be revisited at 1.0, when a release may be supported for longer.

## Scope

In scope is anything in this repository that a server operator or a person
using a server relies on:

- **The server** (`server/`) — the HTTP API, the live connection,
  authentication and sessions, passkeys, second factors and recovery,
  permissions and roles, moderation, the directory and projects, webhooks and
  applications, attachments and storage, and the database schema and
  migrations.
- **The web application** (`apps/web/`) and the **shared interface layer**
  (`packages/`) — cross-site scripting, anything that discloses one person's
  data to another, anything that sends a credential where it should not go.
- **The desktop client** (`apps/desktop/`) — including its updater, the
  signature check on what it downloads, and anything that lets a server or a
  message reach outside the webview.
- **The federation protocol** — both [the specification](docs/federation-protocol.md)
  and this implementation of it: request signing and verification, replay
  protection, event signatures, and anything that lets one server impersonate
  another, forge an event, or read what it was not sent.

Some things are not vulnerabilities in TDC, though a report is still welcome if
you are unsure:

- **A configuration the server warns about.** `server.secure_cookies = false`
  over HTTPS, `server.expose_internal_errors` on a public server, and an unset
  canonical address are all named in the startup log as unsafe. A way to reach
  that state without the warning is in scope.
- **Something an administrator can already do.** An account holding
  `server.configure` changing a setting is the permission working.
- **Third-party services** an operator chooses to connect — a mail server, an
  object store, a GIF provider, a sign-in provider. Report those to whoever
  runs them; how TDC uses them is in scope.
- **Volumetric denial of service** — exhausting a server with ordinary traffic.
  A single request that costs the server far more than it should is in scope.
- **Reports from automated scanners** that do not show an actual effect, and
  missing headers that have no consequence for this application.
- **Social engineering** of maintainers, operators or members.

## Running a server

If you run a TDC server, watch this repository's releases — **Watch → Custom →
Releases** on GitHub — so a security release reaches you when it is published.
The [hosting guide](docs/hosting.md) covers keeping a server current.
