# What this change delivered, and what it did not

Archived 2026-09-22. 195 of 196 tasks done, and the specs are deployed to
`openspec/specs/` as the baseline every later change builds on.

TDC runs: people sign in, talk in channels and threads, share files, find
things, are moderated, and are notified. It works on a phone, on a desktop, and
for a program holding a token. Operators can host it, configure it, back it up
and upgrade it.

## What is still outstanding

**MILESTONE 10** is not met. It asks for a person who has never seen this
repository to deploy a server from the published container image following only
the documentation. That is somebody doing something rather than a thing the
code can be made to do, and claiming it on their behalf would be the kind of
green that means nothing. `v1.0.0-dev` is held unused for it; 10a is tagged
`v1.1.0-dev`.

Everything it needs exists — the image, the compose file, the hosting guide —
and the demonstration is the only part left.

**Four deferrals**, each blocked on something outside this repository:

- **9.14d** Menu mnemonics on Linux. Wails sets `use_underline` only on its
  GTK3 path, so Alt+F does nothing under GTK4. Upstream.
- **9.15a** Window translucency on Linux. Wails' `setTransparent()` is empty on
  its GTK4 path. Upstream, and a minimal Wails application shows the same.
- **9.17a** NVIDIA hardware. The detection branch can only take its false path
  on a machine with Intel graphics. Needs somebody with the card.
- **9c.5a** The date picker is still `@vuepic/vue-datepicker`. It works and is
  themed; replacing it is worth doing when something needs it rather than for
  tidiness.
