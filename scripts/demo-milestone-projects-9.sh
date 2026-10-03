#!/usr/bin/env bash
#
# Projects Milestone 9: a newcomer with only the guide, and nothing broken.
#
# Two things on one server built from this checkout. First, the browser suite
# the foundation change gates releases on — every chat check, unchanged, now
# alongside the projects checks added with this change: the conversation link
# and what it withholds, two people moving one card, a batch partly refused,
# the board and an issue's page audited for accessibility and held to their
# payload budgets. Then, on a fresh database, somebody who registered a minute
# ago follows "From a conversation to done" in docs/projects.md step by step.
#
# WebKit is the one engine of the three that cannot launch on every Linux
# distribution (it wants Debian's ICU); set ENGINES to include it where it can,
# as CI does on ubuntu-latest.
#
# The suite's checks have the suite's own time limits, which a heavily loaded
# machine can exceed; GUIDE_ONLY=1 runs the second half alone.
#
# Usage: scripts/demo-milestone-projects-9.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m9
DEMO_PORT="${TDC_DEMO_PORT:-8409}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"
ENGINES="${ENGINES:-chromium firefox}"

build
AS_CI=1 start_server

FOUNDER="$(sign_in founder)"
api "$FOUNDER" POST /api/rooms '{"slug":"general","name":"General","topic":"A channel to look at"}' >/dev/null

if [[ -z "${GUIDE_ONLY:-}" ]]; then
say "The browser suite: every existing check, and the projects checks with them"
PROJECTS=()
for engine in $ENGINES; do PROJECTS+=("--project=${engine}"); done
(cd "$ROOT/apps/web" && TDC_TEST_URL="$BASE" \
  bunx playwright test --project=setup "${PROJECTS[@]}" --project=arrival --reporter=list) \
  | tee "$WORK/suite.log" | grep -E "✓|✘|passed|failed|skipped|flaky|board:|issue:" || true
SUMMARY="$(grep -E '^\s+[0-9]+ (passed|failed|flaky|skipped)' "$WORK/suite.log" | tr -s ' ' | tr '\n' ' ')"
note "suite: ${SUMMARY}"
# What went wrong, if anything did, before the check that says so.
grep -A30 -E '^\s+[0-9]+\) \[' "$WORK/suite.log" | head -200 || true
check "no check failed" bash -c "! grep -qE '^\s+[0-9]+ failed' '$WORK/suite.log'"
check "and none was flaky" bash -c "! grep -qE '^\s+[0-9]+ flaky' '$WORK/suite.log'"
for spec in messaging message-actions keyboard reconnection theme accessibility parity tour permissions; do
  check "the existing ${spec} checks ran and passed" grep -qE "✓ .*tests/${spec}\.spec\.ts" "$WORK/suite.log"
done
for spec in projects projects-accessibility projects-payload; do
  check "the new ${spec} checks ran and passed" grep -qE "✓ .*tests/${spec}\.spec\.ts" "$WORK/suite.log"
done
grep -hoE "(board|issue): [0-9]+ bytes.*" "$WORK/suite.log" | sed 's/^/  /' || true
fi

say "A fresh server, and somebody who has never seen it"
kill "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""
start_server
FOUNDER="$(sign_in founder)"
GENERAL="$(api "$FOUNDER" POST /api/rooms '{"slug":"general","name":"General","topic":"Everybody"}' | json 'd["id"]')"
register newcomer
NEWCOMER="$(sign_in newcomer)"
api "$NEWCOMER" POST "/api/rooms/${GENERAL}/join" >/dev/null
note "newcomer registered and in General; the founder will say something there"

say "In the browser: the guide's six steps, nothing else"
browser projects-milestone-9

say "From outside the browser"
ISSUE="$(api "$FOUNDER" GET /api/issues/WEB-1)"
check "WEB-1 is Done" test "$(echo "$ISSUE" | json 'd["state"]["name"]')" = Done
check "it was filed by the newcomer" test "$(echo "$ISSUE" | json 'd["author"].split("@")[0]')" = newcomer
check "and remembers the message it came from" \
  test "$(echo "$ISSUE" | json 'd["origin"]["disclosed"]')" = True
MILESTONE="$(api "$FOUNDER" GET /api/projects/WEB/milestones | json '[m for m in d if m["name"]=="Launch"][0]["id"]')"
check "Launch counts 1 of 1 done" \
  test "$(api "$FOUNDER" GET "/api/projects/WEB/milestones/${MILESTONE}/progress" | json 'str(d["completed"])+" of "+str(d["total"])')" = "1 of 1"

say "Projects Milestone 9 demonstrated"
if [[ -z "${GUIDE_ONLY:-}" ]]; then
  note "every existing browser check passed beside the new projects checks (${ENGINES}, and arrival)"
else
  note "the browser suite was not run this time (GUIDE_ONLY)"
fi
note "a newcomer followed the guide alone from a live message to a milestone counted done"
