#!/usr/bin/env bash
#
# Projects Milestone 8: progress.
#
# Eight issues, five of them in a cycle under way, three finished. The project
# and the cycle each report how much is done — counted from the issues — and
# the same figures are recomputed here from nothing but each issue's recorded
# history, and must match exactly. In the browser a card finished on the board
# changes the figures above it without the page loading again, and a browser
# that cannot draw charts still shows every number, as a table.
#
# Usage: scripts/demo-milestone-projects-8.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m8
DEMO_PORT="${TDC_DEMO_PORT:-8408}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

day() { TZ=Africa/Dar_es_Salaam date -d "$1" +%F; }

say "Eight issues, five in Sprint 3, three finished"
FOUNDER="$(sign_in founder)"
api "$FOUNDER" POST /api/projects '{"key":"WEB","name":"Community website","visibility":"public"}' >/dev/null
api "$FOUNDER" PUT /api/projects/WEB/planning '{"timeZone":"Africa/Dar_es_Salaam"}' >/dev/null
SPRINT="$(api "$FOUNDER" POST /api/projects/WEB/cycles \
  "{\"name\":\"Sprint 3\",\"startsOn\":\"$(day '6 days ago')\",\"endsOn\":\"$(day '7 days')\"}" | json 'd["id"]')"
for n in $(seq 1 8); do
  api "$FOUNDER" POST /api/projects/WEB/issues "{\"title\":\"Issue ${n}\"}" >/dev/null
done
api "$FOUNDER" POST /api/projects/WEB/issues/bulk \
  "{\"issues\":[\"WEB-1\",\"WEB-2\",\"WEB-3\",\"WEB-4\",\"WEB-5\"],\"change\":{\"kind\":\"cycle\",\"value\":\"${SPRINT}\"}}" >/dev/null
STATES="$(api "$FOUNDER" GET /api/projects/WEB)"
DONE="$(echo "$STATES" | json '[s["id"] for s in d["states"] if s["name"]=="Done"][0]')"
DOING="$(echo "$STATES" | json '[s["id"] for s in d["states"] if s["name"]=="In progress"][0]')"
for ref in WEB-1 WEB-6 WEB-7; do
  api "$FOUNDER" POST "/api/issues/${ref}/state" "{\"stateId\":\"${DONE}\"}" >/dev/null
done
# One reopened and finished again, and one filed and deleted: the history has
# more in it than the answer, which is what replaying it has to get right.
api "$FOUNDER" POST /api/issues/WEB-6/state "{\"stateId\":\"${DOING}\"}" >/dev/null
api "$FOUNDER" POST /api/issues/WEB-6/state "{\"stateId\":\"${DONE}\"}" >/dev/null
api "$FOUNDER" POST /api/projects/WEB/issues '{"title":"Filed by mistake"}' >/dev/null
api "$FOUNDER" DELETE /api/issues/WEB-9 >/dev/null
note "WEB-1, WEB-6 and WEB-7 are done; WEB-1 to WEB-5 are in Sprint 3; WEB-9 was filed and deleted"

# Counts done and total from each issue's own recorded history, nothing else:
# the last state it was moved to, whether it was deleted, its last cycle.
recount() {
  local scope="$1"
  for n in $(seq 1 9); do
    curl -s -H "Authorization: Bearer ${FOUNDER}" "${BASE}/api/issues/WEB-${n}/history" || echo '[]'
    echo
  done | python3 -c "
import json, sys
scope = sys.argv[1]
total = done = 0
for line in sys.stdin:
    line = line.strip()
    if not line or not line.startswith('['):
        continue
    events = json.loads(line)
    if not events:
        continue
    state, cycle, deleted = 'To do', None, False
    for e in events:
        c = e['content']
        if e['type'] == 'issue.state_changed': state = c['newName']
        if e['type'] == 'issue.cycle_changed': cycle = c.get('new')
        if e['type'] == 'issue.deleted': deleted = True
    if deleted or (scope != 'project' and cycle != scope):
        continue
    total += 1
    done += state == 'Done'
print(f'{done} of {total}')
" "$scope"
}
reported() { api "$FOUNDER" GET "$1" | json 'str(d["completed"])+" of "+str(d["total"])'; }

say "What is reported, and what the history says"
P_REPORTED="$(reported /api/projects/WEB/progress)"
C_REPORTED="$(reported "/api/projects/WEB/cycles/${SPRINT}/progress")"
P_REPLAYED="$(recount project)"
C_REPLAYED="$(recount "$SPRINT")"
check "the project reports ${P_REPORTED}, and its history says ${P_REPLAYED}" test "$P_REPORTED" = "$P_REPLAYED"
check "Sprint 3 reports ${C_REPORTED}, and its history says ${C_REPLAYED}" test "$C_REPORTED" = "$C_REPLAYED"
check "which is 3 of 8 and 1 of 5" test "${P_REPORTED}|${C_REPORTED}" = "3 of 8|1 of 5"
check "and the server's own replayed trend ends on the same figures" \
  test "$(api "$FOUNDER" GET /api/projects/WEB/progress | json 'str(d["trend"][-1]["completed"])+" of "+str(d["trend"][-1]["total"])')" = "$P_REPORTED"

say "In the browser: matching figures, a live move, and no chart"
browser projects-milestone-8 | tee "$WORK/browser.log" | grep -E "✓|✘|passed|failed|project:|after moving|without a chart"

say "From outside, after the move"
P_REPORTED="$(reported /api/projects/WEB/progress)"
C_REPORTED="$(reported "/api/projects/WEB/cycles/${SPRINT}/progress")"
check "the project now reports ${P_REPORTED}, as its history says ($(recount project))" \
  test "$P_REPORTED" = "$(recount project)"
check "Sprint 3 now reports ${C_REPORTED}, as its history says ($(recount "$SPRINT"))" \
  test "$C_REPORTED" = "$(recount "$SPRINT")"
check "which is 4 of 8 and 2 of 5" test "${P_REPORTED}|${C_REPORTED}" = "4 of 8|2 of 5"

say "Projects Milestone 8 demonstrated"
note "the project and its cycle report figures that match a recount from the recorded history exactly"
note "a card finished on the board changed the figures above it with no reload"
note "with charts unable to draw, every number was still on the page, as a table"
