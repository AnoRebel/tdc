#!/usr/bin/env bash
#
# Projects Milestone 4: search and filtering.
#
# Issues about logins are filed in a public project and a private one. A search
# across every project the searcher may see returns them most recently changed
# first; words combined with a state and a label narrow to what matches all
# three; and a private project's issues are absent from every search an
# outsider performs — by words, by filters, by naming the project, and through
# the discussions — while hostile input never makes a search fail.
#
# Usage: scripts/demo-milestone-projects-4.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m4
DEMO_PORT="${TDC_DEMO_PORT:-8404}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

say "Issues about logins, in a public project and a private one"
FOUNDER="$(sign_in founder)"
register outsider
OUTSIDER="$(sign_in outsider)"
api "$FOUNDER" POST /api/projects '{"key":"OPEN","name":"Website","visibility":"public"}' >/dev/null
api "$FOUNDER" POST /api/projects '{"key":"SEC","name":"Security","visibility":"private"}' >/dev/null
BUG="$(api "$FOUNDER" POST /api/projects/OPEN/labels '{"name":"bug"}' | json 'd["id"]')"
DONE="$(api "$FOUNDER" GET /api/projects/OPEN | json '[s["id"] for s in d["states"] if s["name"]=="Done"][0]')"

api "$FOUNDER" POST /api/projects/OPEN/issues '{"title":"Login fails after a password change","description":"Old sessions keep working."}' >/dev/null
api "$FOUNDER" POST /api/projects/OPEN/issues '{"title":"Login page is slow on 3G"}' >/dev/null
api "$FOUNDER" PUT "/api/issues/OPEN-1/labels/${BUG}" >/dev/null
api "$FOUNDER" POST /api/issues/OPEN-1/state "{\"stateId\":\"${DONE}\"}" >/dev/null
sleep 1.1
api "$FOUNDER" PATCH /api/issues/OPEN-2 '{"description":"Three seconds to first paint on a slow login."}' >/dev/null
sleep 1.1
api "$FOUNDER" POST /api/projects/SEC/issues '{"title":"Login bypass","description":"The token in the recovery link is reusable."}' >/dev/null
DISCUSSION="$(api "$FOUNDER" GET /api/issues/SEC-1 | json 'd["id"]')"
api "$FOUNDER" POST "/api/rooms/${DISCUSSION}/messages" '{"body":"Reproduced the login bypass on staging."}' >/dev/null
note "OPEN-1 (done, bug), OPEN-2 (changed later), SEC-1 (private, changed last, with a comment)"

say "In the browser: a member and an outsider search"
browser projects-milestone-4

say "From outside the browser: the same searches through the API"
keys() { json '",".join(i["key"] for i in d["issues"])'; }
check "a member's search is ranked by recency across both projects" \
  test "$(api "$FOUNDER" GET '/api/issues/search?q=login' | keys)" = "SEC-1,OPEN-2,OPEN-1"
check "words, a state and a label together narrow to the one issue matching all three" \
  test "$(api "$FOUNDER" GET '/api/issues/search?q=login&state=Done&label=bug' | keys)" = "OPEN-1"
check "an outsider's search by words never finds the private issue" \
  test "$(api "$OUTSIDER" GET '/api/issues/search?q=login' | keys)" = "OPEN-2,OPEN-1"
check "nor by its own words" \
  test "$(api "$OUTSIDER" GET '/api/issues/search?q=recovery+token' | keys)" = ""
check "nor by filters alone" \
  test "$(api "$OUTSIDER" GET '/api/issues/search?state=To+do' | json '"SEC-1" in [i["key"] for i in d["issues"]]')" = "False"
check "nor by naming the private project" \
  test "$(api "$OUTSIDER" GET '/api/issues/search?project=SEC' | keys)" = ""
check "nor through what was said in its discussion" \
  test "$(api "$OUTSIDER" GET '/api/issues/search?q=staging' | json 'len(d["discussions"])')" = "0"
check "while the member finds that comment" \
  test "$(api "$FOUNDER" GET '/api/issues/search?q=staging' | json 'd["discussions"][0]["issueKey"]')" = "SEC-1"
for hostile in "'" '"unclosed' '&%26|!' ')))' 'login:*' '\\'; do
  check "hostile input $(printf %q "$hostile") is answered, not an error" \
    test "$(status "$OUTSIDER" GET "/api/issues/search?q=$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1]))' "$hostile")")" = "200"
done

say "Projects Milestone 4 demonstrated"
note "search across every visible project, most recently changed first"
note "words combined with a state and a label narrowed correctly"
note "a private project's issues were absent from every search an outsider made"
