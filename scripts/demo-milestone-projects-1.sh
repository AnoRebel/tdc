#!/usr/bin/env bash
#
# Projects Milestone 1: projects exist and issues can be filed.
#
# In the browser, against a real server: a project is started, three states of
# its own configured, an issue filed and moved through all three, and its
# history read back complete and in order. Then, from outside the browser, the
# same history is checked against the event log it comes from: every entry
# signed, in one gapless order, and reproducing the issue's state when replayed.
#
# Usage: scripts/demo-milestone-projects-1.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m1
DEMO_PORT="${TDC_DEMO_PORT:-8401}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

say "In the browser: start a project, configure its states, file an issue and move it"
browser projects-milestone-1

say "From outside the browser: what the pages showed is what the server recorded"
TOKEN="$(sign_in founder)"

ISSUE="$(api "$TOKEN" GET /api/issues/WEB-1)"
check "WEB-1 is in Done" \
  test "$(echo "$ISSUE" | json 'd["state"]["name"]')" = "Done"
check "Done is the state marked finished" \
  test "$(echo "$ISSUE" | json 'd["state"]["isCompletion"]')" = "True"

STATES="$(api "$TOKEN" GET /api/projects/WEB | json '",".join(s["name"] for s in d["states"])')"
check "the project's states are Triage, Review, Done, in that order" \
  test "$STATES" = "Triage,Review,Done"

HISTORY="$(api "$TOKEN" GET /api/issues/WEB-1/history)"
check "the history holds the filing and two moves, in that order" \
  test "$(echo "$HISTORY" | json '",".join(e["type"] for e in d)')" \
    = "issue.created,issue.state_changed,issue.state_changed"
check "each move names what it was before and after" \
  test "$(echo "$HISTORY" | json '";".join(str(e["content"].get("previousName"))+">"+str(e["content"].get("newName")) for e in d[1:])')" \
    = "Triage>Review;Review>Done"
check "the entries are in strictly increasing order" \
  test "$(echo "$HISTORY" | json 'all(a["sequence"]<b["sequence"] for a,b in zip(d,d[1:]))')" = "True"
for method in DELETE PUT PATCH POST; do
  check "the history refuses ${method}: nothing serves a write to it" \
    grep -qE '^(404|405)$' <<<"$(status "$TOKEN" "$method" /api/issues/WEB-1/history '{}')"
done

say "The event log itself: signed, ordered, and replayable into the issue it describes"
# The project's stream is verified with nothing but the keys this server
# publishes — which is all a peer or an auditor would have — and replayed from
# nothing to reproduce the issue's current columns.
(cd "$ROOT/server" && go run ./cmd/tdc-projects-audit \
  --database "postgres://tdc:tdc@127.0.0.1:55433/${DATABASE}?sslmode=disable" \
  --published "${BASE}/.well-known/tdc-federation" \
  --project WEB)

say "Projects Milestone 1 demonstrated"
note "a project was started and its workflow configured to three states in the browser"
note "an issue was filed and moved through all three, and its history read back in order"
note "every event in the project's stream verified against the published keys and replayed exactly"
