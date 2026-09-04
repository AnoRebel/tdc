## What this changes

<!-- One or two sentences. The diff says what; this should say why. -->

## Why this approach

<!--
What problem does it solve, and why is this the right shape of solution?
A reviewer who understands the reasoning can find a mistake in it. One who
only sees the change can check the spelling.
-->

## How it was verified

<!--
Not "tests pass" — what did you actually check?

For a bug fix, the honest check is to revert the fix and watch the test fail.
Say if you did that.
-->

- [ ] `bun run lint`, `bun run typecheck` and `bun run test` pass
- [ ] `gofmt -l .` prints nothing, `go vet ./...` and `go test ./...` pass
- [ ] There is a test that fails without this change
- [ ] Documentation is updated in this pull request, if behaviour changed

## AI assistance

<!--
Tick one. Neither answer counts against a contribution — a reviewer just needs
to know how much of the reasoning is yours, because that changes how the review
is done.
-->

- [ ] No AI tools were used
- [ ] AI tools were used, and I have explained the change in my own words above,
      read the code, and verified it myself

## Anything a reviewer should look at closely

<!-- A decision you were unsure about, a trade-off, something you would like a
     second opinion on. Saying so is not a weakness; it is what review is for. -->
