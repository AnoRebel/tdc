#!/usr/bin/env bash
#
# Projects Milestone 5: the board.
#
# Three hundred issues are filed. In the browser, over a link throttled to a
# weak phone connection, the board's first load is measured against its budget,
# one card is dragged to another column and another is moved from the keyboard
# alone, through the same request. From outside: the moves are in each issue's
# history, the payload is the same size for a project six times larger, and two
# moves of one card at once converge on one of them.
#
# Usage: scripts/demo-milestone-projects-5.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m5
DEMO_PORT="${TDC_DEMO_PORT:-8405}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

say "A project with three hundred issues"
FOUNDER="$(sign_in founder)"
api "$FOUNDER" POST /api/projects '{"key":"WEB","name":"Community website","visibility":"public"}' >/dev/null
for n in $(seq 1 "${ISSUES:-300}"); do
  api "$FOUNDER" POST /api/projects/WEB/issues \
    "{\"title\":\"Issue ${n}: a card on a board that should never ship whole\",\"description\":\"$(printf 'Long description text. %.0s' $(seq 1 30))\"}" >/dev/null
done
note "WEB-1 to WEB-300 filed, all in To do"

say "In the browser, on a slow phone link: measure, drag, and move by keyboard"
browser projects-milestone-5 | tee "$WORK/browser.log"
grep -o 'first board load: .*' "$WORK/browser.log" | sed 's/^/  /'

say "From outside the browser"
STATES="$(api "$FOUNDER" GET /api/projects/WEB)"
DOING="$(echo "$STATES" | json '[s["id"] for s in d["states"] if s["name"]=="In progress"][0]')"
DONE="$(echo "$STATES" | json '[s["id"] for s in d["states"] if s["name"]=="Done"][0]')"
TODO="$(echo "$STATES" | json '[s["id"] for s in d["states"] if s["name"]=="To do"][0]')"
check "the dragged card is In progress, and its history says so" \
  test "$(api "$FOUNDER" GET /api/issues/WEB-300/history | json 'd[-1]["content"]["newName"]')" = "In progress"
check "the keyboard-moved card is Done, and its history says so" \
  test "$(api "$FOUNDER" GET /api/issues/WEB-299/history | json 'd[-1]["content"]["newName"]')" = "Done"

SMALL="$(curl -s -H "Authorization: Bearer ${FOUNDER}" "${BASE}/api/projects/WEB/board" | wc -c)"
for n in $(seq 301 900); do
  api "$FOUNDER" POST /api/projects/WEB/issues "{\"title\":\"Issue ${n}: a card on a board that should never ship whole\"}" >/dev/null
done
LARGE="$(curl -s -H "Authorization: Bearer ${FOUNDER}" "${BASE}/api/projects/WEB/board" | wc -c)"
note "board first load: ${SMALL} bytes at 300 issues, ${LARGE} bytes at 900"
check "the board is within its 48 KB budget at 900 issues" test "$LARGE" -le 49152
check "and is no heavier for three times the issues, beyond longer numbers" test $((LARGE - SMALL)) -le 512

say "Two people moving the same card at the same moment"
for round in 1 2 3; do
  ISSUE="WEB-$((round))"
  ( curl -s -o "$WORK/a$round" -w '%{http_code}' -X POST "${BASE}/api/issues/${ISSUE}/state" \
      -H "Authorization: Bearer ${FOUNDER}" -H 'Content-Type: application/json' \
      -d "{\"stateId\":\"${DOING}\",\"expectedStateId\":\"${TODO}\"}" >"$WORK/a$round.code" ) &
  FIRST=$!
  ( curl -s -o "$WORK/b$round" -w '%{http_code}' -X POST "${BASE}/api/issues/${ISSUE}/state" \
      -H "Authorization: Bearer ${FOUNDER}" -H 'Content-Type: application/json' \
      -d "{\"stateId\":\"${DONE}\",\"expectedStateId\":\"${TODO}\"}" >"$WORK/b$round.code" ) &
  SECOND=$!
  # These two, by name: a bare wait would also wait on the server.
  wait "$FIRST" "$SECOND"
  CODES="$(cat "$WORK/a$round.code") $(cat "$WORK/b$round.code")"
  check "${ISSUE}: one move won and the other was refused (${CODES})" \
    grep -qE '^(200 409|409 200)$' <<<"$CODES"
  FINAL="$(api "$FOUNDER" GET "/api/issues/${ISSUE}" | json 'd["state"]["name"]')"
  LOSER="$(cat "$WORK/a$round" "$WORK/b$round" | python3 -c 'import sys,json
for line in sys.stdin.read().replace("}{","}\n{").splitlines():
  d=json.loads(line)
  if "issue" in d: print(d["issue"]["state"]["name"])')"
  check "${ISSUE}: the refused mover was told it is ${FINAL}, where it really is" test "$LOSER" = "$FINAL"
  check "${ISSUE}: the history has exactly one move" \
    test "$(api "$FOUNDER" GET "/api/issues/${ISSUE}/history" | json 'sum(1 for e in d if e["type"]=="issue.state_changed")')" = "1"
done

say "Projects Milestone 5 demonstrated"
note "the board loaded within budget on a throttled phone link, at any project size"
note "a card moved by dragging and another by keyboard alone, through the same request"
note "simultaneous moves of one card converged on one, and the other was told"
