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

Tool versions are pinned in [`mise.toml`](../mise.toml) — Go 1.27.0, Bun 1.4.2,
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

Do not open a public issue for a vulnerability. Email the maintainer directly
and allow a reasonable window for a fix before disclosing it.

Security fixes are prioritised over everything else, and credit is given unless
you would rather it were not.
