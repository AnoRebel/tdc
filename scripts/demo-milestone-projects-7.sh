#!/usr/bin/env bash
#
# Projects Milestone 7: cycles and milestones.
#
# A two-week cycle runs with six issues in it, on Dar es Salaam's calendar. In
# the browser the founder makes Sprint 1 (ending today) and Sprint 2 (starting
# tomorrow), a milestone whose day has passed, and plans issues from the list,
# the board and an issue's page. Two issues are finished; then the team ends
# the sprint a day early, and the server — by itself, on its minute — deals
# with the end. Under carry-forward every unfinished issue moves to Sprint 2;
# in a second project under remain, every one stays. Nothing changes state and
# nothing disappears in either. The milestone spans both cycles, is overdue,
# and its issues are exactly as they were.
#
# Usage: scripts/demo-milestone-projects-7.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m7
DEMO_PORT="${TDC_DEMO_PORT:-8407}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

# Days in Dar es Salaam's calendar, whatever this machine's zone.
day() { TZ=Africa/Dar_es_Salaam date -d "$1" +%F; }
export SPRINT1_START="$(day '13 days ago')" SPRINT1_END="$(day today)"
export SPRINT2_START="$(day tomorrow)" SPRINT2_END="$(day '14 days')"
export BETA_DAY="$(day '3 days ago')"
YESTERDAY="$(day yesterday)"

say "Six issues in a public project, and a second project that keeps unfinished work"
FOUNDER="$(sign_in founder)"
api "$FOUNDER" POST /api/projects '{"key":"WEB","name":"Community website","visibility":"public"}' >/dev/null
for title in "Sign-up page" "Password reset" "Swahili home page" "Footer links" "Image sizes" "Font choice"; do
  api "$FOUNDER" POST /api/projects/WEB/issues "{\"title\":\"${title}\"}" >/dev/null
done
note "WEB-1 to WEB-6 filed; Sprint 1 runs ${SPRINT1_START} to ${SPRINT1_END}, Sprint 2 from ${SPRINT2_START}"

say "In the browser: the calendar, two cycles, a milestone, and planning three ways"
browser projects-milestone-7-planning

say "Two issues are finished, then the sprint is ended a day early"
WEB="$(api "$FOUNDER" GET /api/projects/WEB)"
DONE="$(echo "$WEB" | json '[s["id"] for s in d["states"] if s["isCompletion"]][0]')"
for ref in WEB-1 WEB-2; do
  api "$FOUNDER" POST "/api/issues/${ref}/state" "{\"stateId\":\"${DONE}\"}" >/dev/null
done
CYCLES="$(api "$FOUNDER" GET /api/projects/WEB/cycles)"
SPRINT1="$(echo "$CYCLES" | json '[c["id"] for c in d if c["name"]=="Sprint 1"][0]')"
SPRINT2="$(echo "$CYCLES" | json '[c["id"] for c in d if c["name"]=="Sprint 2"][0]')"
check "both cycles are the project's days, with Dar es Salaam's offset on their instants" \
  test "$(echo "$CYCLES" | json '",".join(c["endsAt"][-6:] for c in d)')" = "+03:00,+03:00"

# What the issues are before the end, to compare with after.
snapshot() {
  for n in 1 2 3 4 5 6; do
    api "$FOUNDER" GET "/api/issues/WEB-${n}" | json 'd["key"]+":"+d["state"]["name"]+":"+d["updatedAt"]'
  done
}
BEFORE="$(snapshot)"

# A second project under remain, with the same shape of work.
api "$FOUNDER" POST /api/projects '{"key":"OPS","name":"Operations","visibility":"public"}' >/dev/null
api "$FOUNDER" PUT /api/projects/OPS/planning '{"timeZone":"Africa/Dar_es_Salaam","cyclePolicy":"remain"}' >/dev/null
OPS_CYCLE="$(api "$FOUNDER" POST /api/projects/OPS/cycles \
  "{\"name\":\"Week 40\",\"startsOn\":\"${SPRINT1_START}\",\"endsOn\":\"${SPRINT1_END}\"}" | json 'd["id"]')"
api "$FOUNDER" POST /api/projects/OPS/cycles \
  "{\"name\":\"Week 41\",\"startsOn\":\"${SPRINT2_START}\",\"endsOn\":\"${SPRINT2_END}\"}" >/dev/null
for title in "Rotate keys" "Renew certificate" "Back up the database"; do
  api "$FOUNDER" POST /api/projects/OPS/issues "{\"title\":\"${title}\"}" >/dev/null
done
api "$FOUNDER" POST /api/projects/OPS/issues/bulk \
  "{\"issues\":[\"OPS-1\",\"OPS-2\",\"OPS-3\"],\"change\":{\"kind\":\"cycle\",\"value\":\"${OPS_CYCLE}\"}}" >/dev/null
OPS_DONE="$(api "$FOUNDER" GET /api/projects/OPS | json '[s["id"] for s in d["states"] if s["isCompletion"]][0]')"
api "$FOUNDER" POST /api/issues/OPS-1/state "{\"stateId\":\"${OPS_DONE}\"}" >/dev/null

for pair in "WEB:${SPRINT1}:Sprint 1" "OPS:${OPS_CYCLE}:Week 40"; do
  IFS=: read -r key id name <<<"$pair"
  api "$FOUNDER" PATCH "/api/projects/${key}/cycles/${id}" \
    "{\"name\":\"${name}\",\"startsOn\":\"${SPRINT1_START}\",\"endsOn\":\"${YESTERDAY}\"}" >/dev/null
done
note "Sprint 1 and Week 40 now ended at midnight in Dar es Salaam; nobody asks the server to do anything"

say "Waiting for the server to deal with the ends by itself"
for _ in $(seq 1 90); do
  CLOSED="$(api "$FOUNDER" GET /api/projects/WEB/cycles | json '[c.get("closedAt","") for c in d if c["name"]=="Sprint 1"][0]')"
  OPS_CLOSED="$(api "$FOUNDER" GET /api/projects/OPS/cycles | json '[c.get("closedAt","") for c in d if c["name"]=="Week 40"][0]')"
  [[ -n "$CLOSED" && -n "$OPS_CLOSED" ]] && break
  sleep 1
done
check "the server closed Sprint 1 on its own (${CLOSED})" test -n "$CLOSED"
check "and Week 40 (${OPS_CLOSED})" test -n "$OPS_CLOSED"

say "Carry-forward, in WEB"
IN1="$(api "$FOUNDER" GET "/api/projects/WEB/cycles/${SPRINT1}" | json '",".join(sorted(i["key"] for i in d["issues"]))')"
IN2="$(api "$FOUNDER" GET "/api/projects/WEB/cycles/${SPRINT2}" | json '",".join(sorted(i["key"] for i in d["issues"]))')"
check "the finished issues stayed in Sprint 1, where they were finished (${IN1})" test "$IN1" = "WEB-1,WEB-2"
check "every unfinished one is in Sprint 2 (${IN2})" test "$IN2" = "WEB-3,WEB-4,WEB-5,WEB-6"
for n in 3 4 5 6; do
  LAST="$(api "$FOUNDER" GET "/api/issues/WEB-${n}/history" | json '[e["actor"]+" "+e["content"].get("reason","")+" "+e["content"].get("newName","") for e in d][-1]')"
  # The server's own name, with nobody in it: no "@".
  check "WEB-${n}'s own history records the server carrying it to Sprint 2 (${LAST})" \
    grep -qE '^[^@ ]+ cycle_ended Sprint 2$' <<<"$LAST"
done
AFTER="$(snapshot)"
check "no issue changed state, and none was touched beyond its cycle" \
  test "$(echo "$BEFORE" | cut -d: -f1,2)" = "$(echo "$AFTER" | cut -d: -f1,2)"
check "all six issues are still there" test "$(echo "$AFTER" | wc -l)" = 6
ENDED="$(api "$FOUNDER" GET /api/projects/WEB/cycles | json '[c["closedPolicy"] for c in d if c["name"]=="Sprint 1"][0]')"
check "Sprint 1 records the policy it ended under (${ENDED})" test "$ENDED" = carry-forward

say "Remain, in OPS"
OPS_IN="$(api "$FOUNDER" GET "/api/projects/OPS/cycles/${OPS_CYCLE}" | json '",".join(sorted(i["key"] for i in d["issues"]))')"
check "every issue stayed in Week 40, finished or not (${OPS_IN})" test "$OPS_IN" = "OPS-1,OPS-2,OPS-3"
for n in 2 3; do
  check "OPS-${n} has no move recorded at the end" \
    test "$(api "$FOUNDER" GET "/api/issues/OPS-${n}/history" | json 'sum(1 for e in d if e["content"].get("reason")=="cycle_ended")')" = 0
done

say "The milestone"
BETA="$(api "$FOUNDER" GET /api/projects/WEB/milestones | json '[m["id"] for m in d if m["name"]=="Beta"][0]')"
MILESTONE="$(api "$FOUNDER" GET "/api/projects/WEB/milestones/${BETA}")"
check "Beta's day (${BETA_DAY}) has passed and it is overdue" \
  test "$(echo "$MILESTONE" | json 'd["milestone"]["overdue"]')" = True
check "it spans two cycles" \
  test "$(echo "$MILESTONE" | json '",".join(c["name"] for c in d["cycles"])')" = "Sprint 1,Sprint 2"
check "its issues are WEB-1 (done) and WEB-6 (not), untouched by being overdue" \
  test "$(echo "$MILESTONE" | json '",".join(sorted(i["key"]+"="+i["state"]["name"] for i in d["issues"]))')" = "WEB-1=Done,WEB-6=To do"

say "In the browser: the ended cycle, the carried issues, the overdue milestone"
browser projects-milestone-7-ending

say "Projects Milestone 7 demonstrated"
note "a two-week cycle ended with unfinished work; the server carried every unfinished issue forward, recorded as its own act"
note "under remain, every issue stayed in the ended cycle; under either, no state changed and nothing was removed"
note "a milestone spanning two cycles, its day passed, shows overdue with its issues as they were"
