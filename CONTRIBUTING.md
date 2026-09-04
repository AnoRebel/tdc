# Contributing to TDC

Thank you for wanting to work on this. TDC is built for the Tanzania Developers
Community, and it is better for every person who improves it.

## How decisions get made

This project has a single maintainer, [@AnoRebel](https://github.com/AnoRebel),
who has final say on what goes in. That is a deliberate choice rather than an
oversight: a small project with one clear direction ships, and one with a
committee argues. Disagreement is welcome and is often how a proposal gets
better — but somebody has to decide, and here that is Ano.

In practice this means:

- **Every change arrives as a pull request.** No exceptions, including for the
  maintainer: a change that skipped review is a change nobody read.
- **Only the maintainer merges.** Approval from anyone else is valuable input,
  not permission.
- **`main` is protected.** Nothing is pushed to it directly.
- **CI must pass before a merge.** A red build is not merged and then fixed;
  it is fixed and then merged.

## If your need is not this project's need

Not every good idea belongs in TDC. This is a platform for one community, with
a deliberately small dependency tree and a bias towards what a self-hoster can
run for nothing — and a change that serves your situation may make it worse for
that goal.

If a proposal is turned down on those grounds, it is not a judgement of the
idea. **Fork it.** Apache-2.0 exists precisely so that you can, and the licence
sets out what that involves: keep the `LICENSE` and `NOTICE` files, state the
changes you made, and keep the existing copyright and attribution notices. You
may then run it, modify it, and distribute it — commercially or not — and you
need not contribute anything back.

Two things the licence asks that are easy to miss. It grants you a patent
licence from every contributor, and that grant terminates if you bring patent
litigation over this software. And it gives you no right to the TDC name or
marks: call your fork something else, so that nobody mistakes it for this
project or expects support here for it.

A fork is a legitimate outcome and not a failure of the conversation.

A fork is also often the better route for something large or speculative:
build it there, run it in earnest, and come back with a proposal that has
evidence behind it. A change that has been used is much easier to accept than
one that has been imagined.

## Where to branch from

```
main   ← released, protected, maintainer-only
beta   ← what is nearly released
dev    ← where work lands first
```

Branch from `dev` and open your pull request against `dev`. Changes move
`dev` → `beta` → `main` as they prove themselves, so a fix reaches people
through that path rather than jumping the queue.

Name a branch for what it does: `fix/attachment-download-headers`,
`feature/channel-bans`. The name is read far more often than it is typed.

## Before you open a pull request

Run what CI runs, so the build is not the first thing to tell you:

```sh
bun install
bun run lint          # ESLint over the TypeScript and Vue
bun run typecheck     # vue-tsc and tsc
bun run test          # the front-end suites

cd server
gofmt -l .            # must print nothing
go vet ./...
go test ./...         # needs a Postgres; see below
```

The Go tests need a database. `docker compose up -d database` provides one, and
`.env.example` has the connection string the tests expect. Tests that need an
object store or a link-preview target skip themselves when those are absent, so
you are not blocked on running everything.

## What a good pull request looks like

**Say why, not only what.** The diff already says what changed. The description
should say what problem it solves and why this is the right shape of solution.
A reviewer who understands the reasoning can spot a mistake in it; one who only
sees the change can only check the spelling.

**Keep it to one thing.** A pull request that fixes a bug and reformats forty
files is two pull requests, and the reformatting hides the fix.

**Include a test that fails without your change.** Not a test that passes
either way — one that demonstrates the problem. For a bug fix, the honest check
is to revert your fix and watch the test fail.

**Match the surrounding code.** Comment density, naming, structure. A file
should not announce that two people wrote it.

**Comments explain why.** The code says what it does. A comment earns its place
by saying what the code cannot: why this approach, what breaks without it, what
was tried and rejected.

## Using AI assistance

You may use AI tools to help write your contribution. Many people do, and used
well they are genuinely useful.

**If you do, say so in the pull request, and explain the change in your own
words.**

This is not a barrier and not a judgement. It exists because a reviewer needs
to know how much to trust the reasoning behind a change. A person who has
thought a problem through can answer a question about it; generated code that
nobody has read cannot, and reviewing it is a different and slower job. Saying
which it is up front is a courtesy to whoever reviews it.

So a pull request that used AI assistance should include:

- **that you used it** — a sentence is enough;
- **your rationale** — why this change, in your own words, not the tool's;
- **what you verified yourself** — that you ran it, that the test genuinely
  fails without the fix, that you understand what the code does.

A contribution that discloses AI use and explains itself clearly is welcome.
One that does not disclose it, and turns out to be unreviewed generated code,
wastes a reviewer's evening — which is the only thing this policy is trying to
prevent.

## Reporting a security problem

Please do not open a public issue for a vulnerability. Email the maintainer
directly, and give a reasonable window for a fix before disclosing it.

Security fixes are prioritised over everything else, and credit is given
unless you would rather it were not.

## Licence

TDC is licensed under Apache-2.0; the full text is in [`LICENSE`](LICENSE).

By opening a pull request you are, in the licence's own terms, deliberately
submitting your work for inclusion — which licenses it under the same terms and
grants the patent licence Apache-2.0 describes. There is no separate agreement
to sign.

You keep the copyright in what you write. If a change is substantial enough
that you would like to be named in [`NOTICE`](NOTICE), say so in the pull
request.
