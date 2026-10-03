#!/usr/bin/env bash
#
# Projects Milestone 3: assignment, labels, priority and relationships.
#
# In the browser: an issue is assigned to two people, given two labels and a
# priority, made the parent of another and the blocker of a third; a blocking
# relationship that would close a loop is refused with the loop named; and the
# child's completion shows on the parent. From outside: both people were told,
# and the refused loop left nothing behind.
#
# Usage: scripts/demo-milestone-projects-3.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m3
DEMO_PORT="${TDC_DEMO_PORT:-8403}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

say "A project, two members, and three issues to relate"
FOUNDER="$(sign_in founder)"
for person in amina bola; do register "$person"; done
api "$FOUNDER" POST /api/projects '{"key":"WEB","name":"Community website","visibility":"public"}' >/dev/null
for person in amina bola; do
  api "$FOUNDER" PUT "/api/projects/WEB/members/${person}" '{"role":"member"}' >/dev/null
done
for title in "Launch the new homepage" "Write the homepage copy" "Move the DNS"; do
  api "$FOUNDER" POST /api/projects/WEB/issues "{\"title\":\"${title}\"}" >/dev/null
done
note "WEB-1, WEB-2 and WEB-3 filed; amina and bola are members"

say "In the browser: assign, label, prioritise, relate — and try to close a loop"
browser projects-milestone-3

say "From outside the browser: what the page did is what the server holds"
ISSUE="$(api "$FOUNDER" GET /api/issues/WEB-1)"
check "WEB-1 is assigned to amina and bola" \
  test "$(echo "$ISSUE" | json '",".join(sorted(a["username"] for a in d["assignees"]))')" = "amina,bola"
check "it carries two labels" \
  test "$(echo "$ISSUE" | json '",".join(sorted(l["name"] for l in d["labels"]))')" = "bug,frontend"
check "and the priority Urgent" \
  test "$(echo "$ISSUE" | json 'd["priority"]["name"]')" = "Urgent"
check "its child is done, and the parent shows 1 of 1" \
  test "$(echo "$ISSUE" | json '(d["relations"]["childProgress"]["completed"], d["relations"]["childProgress"]["total"])')" = "(1, 1)"
check "it blocks WEB-3" \
  test "$(echo "$ISSUE" | json '[b["key"] for b in d["relations"]["blocks"]]')" = "['WEB-3']"
check "the refused loop left WEB-3 blocking nothing" \
  test "$(api "$FOUNDER" GET /api/issues/WEB-3 | json 'len(d["relations"]["blocks"])')" = "0"
check "and the same loop asked for through the API is refused too" \
  test "$(status "$FOUNDER" POST /api/issues/WEB-3/relations '{"kind":"blocks","issue":"WEB-1"}')" = "409"

for person in amina bola; do
  TOLD="$(api "$(sign_in "$person")" GET '/api/me/notifications?grouped=false' \
    | json "sum(1 for n in d if n['reason']=='assigned' and 'WEB-1' in n.get('detail',''))")"
  check "${person} was told WEB-1 is theirs" test "$TOLD" = "1"
done

say "Projects Milestone 3 demonstrated"
note "two people assigned and both told; two labels and a priority set"
note "a child's completion reflected on its parent"
note "a blocking relationship recorded, and the loop that would have closed it refused"
