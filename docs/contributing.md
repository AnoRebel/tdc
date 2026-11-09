# Contributing to TDC

This is the long version. [`CONTRIBUTING.md`](../CONTRIBUTING.md) at the root
covers the same ground more briefly; where the two differ, this document is the
one that has been checked against the repository.

TDC is built for the Tanzania Developers Community, and it is better for every
person who improves it. What follows is what a contribution has to satisfy and,
more usefully, why each rule exists — a rule whose reason is known can be
applied sensibly to a case nobody anticipated.

## Who decides

The project has one maintainer, [@AnoRebel](https://github.com/AnoRebel), who
has final say on what goes in. That is deliberate. A small project with one
clear direction ships; one run by committee argues. Disagreement is welcome and
is often how a proposal improves, but somebody has to decide, and here that is
Ano.

Four things follow from it, and none of them has an exception:

**Every change arrives as a pull request.** Including the maintainer's own. A
change that skipped review is a change nobody read, and the fact that the author
was trusted does not make the diff correct.

**Only Ano merges.** An approving review from anybody else is valuable input.
It is not permission.

**Only Ano pushes to `main`.** The branch is protected, and nothing reaches it
except through a merge.

**CI must be green before a merge.** A red build is not merged and then fixed.
It is fixed and then merged. The difference matters because the branch is what
everybody else branches from, and a broken one costs every contributor the time
it takes them to work out that the failure is not theirs.

## Where to branch from

```
main   ← released, protected, maintainer-only
beta   ← what is nearly released
dev    ← where work lands first
```

Branch from `dev`, and open the pull request against `dev`. Changes move
`dev` → `beta` → `main` as they prove themselves. A fix reaches people by
travelling that path, not by jumping the queue into `main`.

The three branches deliberately do not ask the same of a change. `dev` runs the
lighter set of checks, because that is where work arrives and a contributor
waiting on a full matrix before a first review is a contributor who stops
contributing. The checks tighten as a change moves towards release, so that what
is expensive to run is run on the smaller number of changes that have already
survived review. A change is therefore not finished when it merges to `dev`; it
is finished when it has been through `beta` without anybody finding a reason to
stop it.

Concretely, every branch runs the web and shared-package checks, the
accessibility checks, and the server's formatting, vet, race-enabled tests and
build. A pull request against `beta` or `main` additionally runs the desktop
build, a dependency licence audit, a vulnerability scan, and a check that every
migration applies cleanly to an empty database. Those four are the slow ones —
the desktop job spends several minutes installing GTK and WebKit before it
compiles anything — and they guard things that change rarely, which is why they
wait for the branches other people actually run.

One status check, named `CI`, aggregates the rest, so branch protection has a
single name to require rather than a list that goes stale whenever a job is
added. It treats a skipped job as a pass, because a skip is the fast tier
deliberately not running the heavy work; a failure or a cancellation is not.

Name a branch for what it does — `fix/attachment-download-headers`,
`feature/channel-bans`. A branch name is read many more times than it is typed.

## Licensing, and the code you may not bring with you

TDC is licensed under Apache-2.0. The full text is in [`LICENSE`](../LICENSE).

By opening a pull request you are, in the licence's own words, deliberately
submitting your work for inclusion — which licenses it under those same terms
and grants the patent licence Apache-2.0 describes. There is no separate
agreement to sign, and you keep the copyright in what you write.

**Everything contributed must be compatible with Apache-2.0.** That applies to
code you write, to code you adapt, and to any dependency you add.

### No GPL or AGPL code, from anywhere

Do not import, copy, adapt or vendor code licensed under the GPL or the AGPL
into this repository. This holds for any source, and it holds specifically for
other TDC repositories: some of the community's earlier work is GPL-licensed,
and the fact that it is the same community's code changes nothing about what its
licence requires.

The reason is not distaste for those licences. It is that they are copyleft, and
Apache-2.0 is not. GPL code brought into an Apache-2.0 project makes the
combined work subject to the GPL, which would silently relicense everything
around it and break the promise made to everybody who took this software on
Apache-2.0 terms. That is not a thing that can be fixed after the fact by
deleting the file: once a release has gone out, the licensing question follows
it.

If you need something a GPL project does, the options are to write it, to find
an implementation under a permissive licence, or to run the GPL software as a
separate process and talk to it over an interface. What is not an option is
pasting it in.

The same care applies to a dependency. Before adding one, check its licence and
the licences of what it pulls in.

Continuous integration audits Go dependency licences on `beta` and `main`,
across both the server and the desktop client, and fails on anything classified
as forbidden or restricted. That check is a safety net rather than the rule: it
sees dependencies, not a file somebody pasted in, and it only runs once a change
has already been through `dev`.

### Attribution

Adapting permissively licensed code from elsewhere is fine, and carries an
obligation: keep the original copyright and licence notice with it, and say in
the pull request where it came from. If a change is substantial enough that you
would like to be named in [`NOTICE`](../NOTICE), say so.

## AI-assisted contributions

You may use AI tools to help write your contribution. Many people do, and used
well they are genuinely useful. This project has no interest in pretending
otherwise.

**If you use them, say so in the pull request, and give your rationale for the
change in your own words.**

Both halves are required, and the second is the one that does the work. The
disclosure tells a reviewer how much of the reasoning is yours; the rationale is
the reasoning itself. A reviewer reading a patch is trying to find the mistake
in the thinking behind it, and they can only do that when the thinking is
written down. Generated code that nobody has read has no thinking behind it to
examine, and reviewing it is a slower and much less pleasant job — done blind,
because the usual shortcut of asking the author what they were trying to achieve
does not work.

So a pull request that used AI assistance says:

- **that AI was used** — one sentence is enough, and nothing rides on which
  tool;
- **the rationale** — what problem this solves and why this is the right shape
  of solution, in your words rather than the tool's;
- **what you verified yourself** — that you ran it, that the test genuinely
  fails without the change, that you understand what the code does.

The pull request template prompts for all of this, so following it is enough.

None of this counts against a contribution. A pull request that discloses AI use
and explains itself clearly is welcome and is reviewed like any other. What is
not welcome is undisclosed generated code that turns out on inspection to be
unreviewed, because that costs a reviewer an evening and produces nothing
anybody can maintain.

## Commit messages

Write the commit message for somebody reading `git log` in two years with no
access to anything but this repository.

**Describe the change to the software and the reason for it.** The subject line
says what changed; the body says why, and what the alternative was if the choice
was not obvious.

**Do not reference internal task numbers, specification identifiers, planning
documents, or the tooling used to produce the change.** An identifier like
`10.16` or a spec name means nothing to a reader who does not have the document
it points at, and the document will not necessarily still exist. A commit that
says "implements 7.9a" has recorded nothing about the software. Say what the
software now does that it did not do before.

The same rule holds for pull request descriptions and for comments in the code.

## Before you open a pull request

Run what CI runs, so that the build is not the first thing to tell you:

```sh
bun install
bun run lint          # ESLint over the TypeScript and Vue
bun run typecheck     # vue-tsc and tsc
bun run test          # the front-end suites

cd server
gofmt -l .            # must print nothing
go vet ./...
golangci-lint run     # the linters in server/.golangci.yml
go test ./...
```

The Go linters are chosen by running each candidate against this codebase
rather than by reputation, and [`server/.golangci.yml`](../server/.golangci.yml)
says why each is there. Beyond the defaults it adds `errorlint`, which catches
an error compared with `==` that will stop matching the day it reaches the
comparison wrapped; `errname`, so a sentinel can be found by the name people
look for; and `predeclared`. Two real bugs came out of enabling it.

If a finding is wrong rather than inconvenient, silence that one line with a
`//nolint:<linter> // <reason>` comment saying why, rather than widening the
configuration — an exclusion that covers a whole file also hides the next
finding in it.

The Go tests need a Postgres. `docker compose up -d database` provides one, and
[`.env.example`](../.env.example) carries the connection string they expect —
note that the port is 55433. Tests that need an object store or a link-preview
target skip themselves when those are absent, so a missing optional service does
not block you.

Tool versions are pinned in [`mise.toml`](../mise.toml) — Go 1.27.3, Bun 1.4.4,
and the `sqlc`, `air`, `golangci-lint` and `task` versions that go with them.
`mise install` after cloning gets the same toolchain everybody else has.
Continuous integration deliberately differs on one point: it takes the latest
Bun rather than the pinned one, so that an incompatibility is found by a build
here rather than by a contributor months later. Go stays pinned there, because a
Go release can change what `vet` objects to and turn a branch red for reasons
that have nothing to do with the change under review.

If you are changing the desktop application, build it from `apps/desktop` with:

```sh
go run github.com/wailsapp/wails/v3/cmd/wails3 task build
```

On Linux that produces a GTK4 build. A second Linux build is published for
distributions that do not have GTK4, and you can produce it with:

```sh
EXTRA_TAGS=gtk3 go run github.com/wailsapp/wails/v3/cmd/wails3 task build
```

It needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` installed. Continuous
integration builds it on Ubuntu 22.04 and checks two things: that it links GTK3
rather than GTK4, and that the highest glibc version it requires is one that
Ubuntu 22.04 actually has. A change that quietly raises that floor makes the
build useless for the only machines it exists for, so the check fails rather
than publishing something that cannot start.

`bun install` also installs the git hooks. Before a commit they check
formatting, run `go vet`, and check that the version in `VERSION` agrees with
everywhere else it is written; before a push they run the Go tests and build.
They are deliberately the cheap checks — anything slow belongs in CI, where it
does not stand between somebody and their own commit.

## The browser checks

The unit tests run in jsdom, which has no layout: it cannot say whether a
control is visible, what colour it resolves to, or where Tab goes. Those
questions need a browser, so they live in `apps/web/tests` and run under
Playwright against a built application and a running server.

They need three things set up, in this order:

```sh
# 1. A database of their own, so seeding is repeatable.
docker compose up -d database
docker exec tdc-database-1 psql -U tdc -d postgres -c "CREATE DATABASE tdc_e2e;"

# 2. The application, built to call the server it is served from.
NUXT_PUBLIC_API_BASE="" bun run --cwd apps/web generate

# 3. The server, serving that build.
cd server && go build -o /tmp/tdc-server ./cmd/tdc-server
TDC_DATABASE_URL="postgres://tdc:tdc@127.0.0.1:55433/tdc_e2e?sslmode=disable" \
TDC_SERVER_WEB_ROOT="$PWD/../apps/web/.output/public" \
TDC_SERVER_ADDRESS="127.0.0.1:8099" /tmp/tdc-server
```

`NUXT_PUBLIC_API_BASE=""` is the one that catches people. Without it the build
bakes in `http://localhost:8080` and every request from the browser goes
somewhere nothing is listening, which presents as "Could not reach the server"
on the sign-in screen rather than as a configuration mistake. The server also
reads the web root once at startup, so regenerating the application means
restarting it.

Then seed an account and a channel — [the workflow](../.github/workflows/ci.yml)
does exactly this under "Prepare an account to sign in with" — and run:

```sh
bun run --cwd apps/web test:e2e     # every engine, as CI runs it
bun run --cwd apps/web test:a11y    # the accessibility pass alone
```

**WebKit needs libraries a Debian-family machine has**: `libicu 74` and flite's
voice libraries, by their Debian sonames. `bunx playwright install --with-deps
webkit` supplies them where those package names exist. On a distribution
shipping a different ICU it refuses to launch and every test "fails" in
milliseconds with a message about the host rather than the application — run
the other two engines locally and let CI cover WebKit.

**Sign-ins are limited to eight per account per quarter hour**, and
registrations to five per address per hour. Both limits are right, so the suite
signs in once and everything else reuses the stored session. A check that signs
in for itself spends one of the eight; a suite that does it per test runs out
and then reports the application as broken when what broke was the suite.

## The milestone demonstrations

Each phase ends with a demonstration rather than a checklist: something that
runs, end to end, and shows the phase did what it promised. They live in
`scripts/` and are run deliberately rather than in continuous integration,
because several need a display, a real browser, or minutes of wall clock.

```sh
python3 scripts/demo-milestone-7.py      # moderation, limits, the audit log
bun run --cwd apps/web demo:milestone-8  # installing, offline, notifications
scripts/demo-milestone-10a.sh            # a program asking a channel a question
```

The later ones are named for their area, `scripts/demo-milestone-<area>-<n>.sh`
— `events-1` to `events-5`, `calls-1` to `calls-7`, `projects-*`,
`federation-*`, `directory-*`, `integrations-*`, `project-federation-*`. Each
builds the server from the checkout and serves its own copy of the web client
(`NUXT_PUBLIC_API_BASE="" bun run generate` in `apps/web` first, or
`SKIP_WEB_BUILD=1` to reuse a build), and runs a Playwright spec from
`apps/web/milestones/`.

Each brings up what it needs and tears it down afterwards. They want the
database container running (`docker compose up -d database`) and nothing else.

Some calls demonstrations need a network shaped a particular way, and build it
without root: `calls-1` (two networks that can reach a relay but not each
other) and `calls-6` and `calls-7` (a connection throttled with `tc`) run in
network namespaces made with `unshare`, reaching the database through a socket
bridged in with `socat`.

**Where a demonstration cannot show something, it says so rather than
skipping it.** Milestone 8 cannot show a push arriving with the browser
genuinely closed, because that needs a push service the machine can reach;
milestone 10a presses one button in a real browser and the other through the
desktop's Go service, because the desktop reaches the server over Wails
bindings rather than HTTP — driving a browser against its built frontend would
exercise a transport it never uses. The seam is named in each case, which is
the difference between a demonstration and a performance.

## Words people read

Every sentence the interface shows goes through the translation layer. TDC is
built for a community that speaks Kiswahili, and a string written straight into
a component is one no translation can reach.

In a template:

```vue
<script setup lang="ts">
const { $t, $ts } = useI18n();
</script>

<template>
  <h2>{{ $t("settings.title") }}</h2>
  <Input :placeholder="$ts('search.placeholder')" />
</template>
```

`$t` for text, `$ts` where a plain string is required — a bound attribute such
as `:label`, `:placeholder` or `:aria-label`. Values interpolate with braces:
`"greeting": "Habari, {name}"`, called as `$t("app.greeting", { name })`.

Add the key to **both** [`packages/ui/locales/en.json`](../packages/ui/locales/en.json)
and [`sw.json`](../packages/ui/locales/sw.json). A key in one and not the other
fails `translations.test.ts`, which also checks that a translation interpolates
the same placeholders as its English — a translation that drops `{name}` loses
the value it was carrying, and one that invents a placeholder renders it
literally.

If you do not speak Kiswahili, say so in the pull request and leave the English
text in the `sw` entry rather than guessing or running it through a machine. A
reviewer who speaks it can correct one string easily; a plausible-looking wrong
translation is harder to spot than an obviously untranslated one.

`bun run --filter=@tdc/web translations` counts what is still written inline. It
reports rather than fails — the number going down is the point.

## The documentation site

The guides in `docs/` are published as a site as well as shown in the
application's help centre: [tdc.anorebel.net](https://tdc.anorebel.net) for
`main`, `tdc-beta.anorebel.net` for `beta` and `tdc-dev.anorebel.net` for
`dev`, with a switcher in the header to move between them. The site is
`apps/docs`, built with Docus, and it reads `docs/` directly — there is no
copy, so a guide is edited in one place whichever way it is edited.

**Editing.** Three ways, all ending in a commit to the branch whose version you
were reading:

- in your editor, in a pull request like any other change;
- with **Edit on GitHub** on a page, which opens GitHub's editor on that
  version's branch;
- with **Edit in Studio**, where a deployment has Studio switched on: sign in
  with GitHub, edit the page in place, and publish. Studio commits as you, so
  it needs your write access to the repository, and on the `dev` site it
  commits to `dev`.

Whichever way, a guide stays plain GitHub-flavoured markdown, because the
help centre renders it with a markdown parser that knows nothing of the site.
Studio's editor is configured not to offer components, styled text or media.
It does save a page's title and description as front matter at the top of the
file; the help centre skips front matter, so that is harmless, and a test fails
if a guide ever contains a component block.

A guide appears on the site as soon as it is in `docs/`. Where it sits in the
sidebar, and its short name, come from the help centre's own list in
`packages/ui/app/lib/guides.ts` and the English translations — add a guide
there and both places pick it up. The previous and next links at the foot of
each guide follow the same order, crossing from one group into the next. Links between guides (`hosting.md#tls`) work
on the site as they do in the application and on GitHub, and a link to another
file in the repository opens it on the version's own branch.

**Running it locally.** `bun run dev:docs` serves it on port 9003 and reloads
when a guide changes; `bun run build:docs` is the production build CI runs.
The version it presents is `DOCS_VERSION` (default `dev`).

**How it is deployed.** The same app three times, each built from its own
branch with `apps/docs/Dockerfile` and the repository root as build context,
and each told which branch it is with the build argument `DOCS_VERSION`. The
container listens on port 3000 and is configured at runtime:

| Variable                                                 | What it is                                                                                                                                         |
| -------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `NUXT_PUBLIC_DOCS_VERSIONS`                              | Where each version lives, as JSON: `[{"version":"main","label":"Stable","url":"https://tdc.anorebel.net"}, …]`. Defaults to the three hosts above. |
| `STUDIO_GITHUB_CLIENT_ID`, `STUDIO_GITHUB_CLIENT_SECRET` | A GitHub OAuth app for this deployment. Leave both unset and Studio is off.                                                                        |
| `NUXT_STUDIO_AUTH_SESSION_SECRET`                        | At least 32 random characters (`openssl rand -hex 32`). Required when Studio is on; the server refuses to start without it.                        |
| `STUDIO_GITHUB_MODERATORS`                               | Optional: the GitHub email addresses allowed to sign in, comma-separated.                                                                          |
| `NUXT_UMAMI_SITE_ID`, `NUXT_RYBBIT_SITE_ID`              | Optional: this deployment's site ids on umami.anorebel.net and rybbit.anorebel.net. Each analytics script is added only when its id is set.        |

Each GitHub OAuth app's callback URL is the deployment's address followed by
`/__nuxt_studio/auth/github`. Studio asks for the `repo` scope, because the
repository is private: that lets the signed-in editor's token read and write
every repository they can, for as long as the session lasts, which is worth
knowing before signing in.

**Search engines and link previews.** Only the stable site is indexed. A build
with `DOCS_VERSION` other than `main` serves a robots.txt of `Disallow: /`,
marks every page `noindex`, and gives each page the stable site's address as
its canonical one, taken from the `main` entry of `NUXT_PUBLIC_DOCS_VERSIONS`.
Every site has a sitemap at `/sitemap.xml`. A page's description is the guide's
first paragraph, and a shared link shows one static picture, `apps/docs/public/og.png`:
the layer's per-page pictures are drawn only when prerendering, which this site
does not do. The icons in `apps/docs/public` are the application's mark,
`apps/desktop/build/icons/tdc.svg`; make them again from it if it changes.

## Screenshots in the guides

The pictures in the guides are made by a script rather than by hand, so that
after a change to the interface every one of them can be made again:

```sh
docker compose up -d database
bun run --cwd apps/web docs:screenshots
```

It builds the server and the application, starts the server on an empty
database of its own (`tdc_docs_screenshots`), fills it with a made-up developer
community — people, channels and conversations, a project with issues, directory
profiles, a webhook and a bot — and photographs each screen a guide describes,
in a light theme at 1280 × 800 (390 × 844 for the phone). A second server, on a
database of its own, is the small community in Arusha that the projects guide's
screens of a project shared between two servers are taken on. The servers are
named `community.localhost` and `arusha.localhost`, so each can reach the other,
and every picture shows them as `community.example` and `arusha.example`. The
last of those pictures stops the first server for a moment, so they are taken
after every other. The images are written
to `docs/images/<guide>/<name>.webp`, with their sizes in
`docs/images/sizes.json`, and the database is dropped afterwards. `git diff
--stat docs/images` then shows which pictures changed; look at them before
committing.

- `--skip-build` reuses the last builds; `--only=projects` or
  `--only=projects/board` takes one guide's pictures, or one picture.
- `--serve` seeds and then leaves the server running to look around in, until
  Ctrl-C; `--attach`, from a second terminal, photographs that server without
  seeding again — the quick loop while working on one picture.
- `TDC_DOCS_POSTGRES` names another Postgres server, `TDC_DOCS_PORT` another
  port (8131, the one after it for webhooks and translation, and the one after
  that for the second server).

Each picture is an entry in `apps/web/scripts/docs-shots.ts` — what to open and
what to press — and the community it shows is `apps/web/scripts/docs-seed.ts`.
Nothing in it is real, and it is made through the API, as a person would make
it. A guide shows a picture with plain markdown, after the paragraph it
illustrates:

```md
![The board, with a column for each state and a card for each issue](images/projects/board.webp)
```

The words in the brackets are what somebody who cannot see the picture reads
instead, so they say what the picture shows rather than naming it. A test checks
that every picture a guide names exists, has such a description, and has its
real size recorded. The help centre loads each picture only when it is scrolled
to, and the documentation site serves `docs/images` at `/images`.

## What a good pull request looks like

**Say why, not only what.** The diff already says what changed.

**Keep it to one thing.** A pull request that fixes a bug and reformats forty
files is two pull requests, and the reformatting hides the fix.

**Include a test that fails without the change.** Not one that passes either
way. For a bug fix the honest check is to revert the fix and watch the test
fail; say in the pull request that you did.

**Update the documentation in the same change.** A setting, an endpoint, a
command or a constraint that arrives without its documentation arrives with its
reasons still only in somebody's head, and reconstructing them later from code
that no longer remembers them loses most of the value.

**Match the surrounding code.** Comment density, naming, structure. A file
should not announce that two people wrote it.

**Comments explain why.** The code already says what it does. A comment earns
its place by saying what the code cannot: why this approach, what breaks without
it, what was tried and rejected.

## If your need is not this project's need

Not every good idea belongs here. TDC serves one community, keeps a deliberately
small dependency tree, and is biased throughout towards what somebody can
self-host for nothing. A change that serves your situation may make it worse
against those goals, and it can be turned down on that basis without being a bad
idea.

If that happens, **fork it**. Apache-2.0 exists precisely so that you can, and
it sets out what that involves: keep `LICENSE` and `NOTICE`, state the changes
you made, and keep the existing copyright and attribution notices. You may then
run, modify and distribute it, commercially or not, and you owe nothing back.

Two things in the licence are easy to miss. It grants you a patent licence from
every contributor, and that grant terminates if you bring patent litigation over
this software. And it gives you no right to the TDC name or marks — call your
fork something else, so that nobody mistakes it for this project or comes here
expecting support for it.

A fork is often the better route for something large or speculative anyway.
Build it there, run it in earnest, and come back with a proposal that has
evidence behind it. A change that has been used is far easier to accept than one
that has been imagined.

## Reporting a security problem

Do not open a public issue, discussion or pull request for a vulnerability.
Report it privately through GitHub's private vulnerability reporting, as the
[security policy](../SECURITY.md) describes — it also covers what happens after
you report, how long before you may disclose, which versions are supported and
what is in scope.

Security fixes are prioritised over everything else, and credit is given unless
you would rather it were not.
