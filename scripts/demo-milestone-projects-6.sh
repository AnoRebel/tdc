#!/usr/bin/env bash
#
# Projects Milestone 6: saved views, and one change to many issues.
#
# A public project and a private one, both with bugs. In the browser the
# founder saves a filtered board and a search across projects and shares both;
# amina, who is in neither project, opens them and sees the board's bugs and
# only the bugs she may see. Then she labels a mixed batch — two issues she
# wrote, two she did not — and reads what became of each. From outside: each
# view is stored as its question, the changed issues each have their own event,
# and the refused ones have none.
#
# Usage: scripts/demo-milestone-projects-6.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m6
DEMO_PORT="${TDC_DEMO_PORT:-8406}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

say "Two projects, and somebody in neither"
FOUNDER="$(sign_in founder)"
register amina
AMINA="$(sign_in amina)"
api "$FOUNDER" POST /api/projects '{"key":"WEB","name":"Community website","visibility":"public"}' >/dev/null
api "$FOUNDER" POST /api/projects '{"key":"SEC","name":"Security fixes","visibility":"private"}' >/dev/null
WEB_BUG="$(api "$FOUNDER" POST /api/projects/WEB/labels '{"name":"bug","colour":"#dc2626"}' | json 'd["id"]')"
api "$FOUNDER" POST /api/projects/WEB/labels '{"name":"triage"}' >/dev/null
SEC_BUG="$(api "$FOUNDER" POST /api/projects/SEC/labels '{"name":"bug"}' | json 'd["id"]')"

api "$FOUNDER" POST /api/projects/WEB/issues '{"title":"The signup form loses what was typed"}' >/dev/null
api "$FOUNDER" POST /api/projects/WEB/issues '{"title":"Dates show in the wrong zone"}' >/dev/null
api "$FOUNDER" POST /api/projects/WEB/issues '{"title":"Write the about page"}' >/dev/null
api "$FOUNDER" PUT /api/projects/WEB/members/amina '{"role":"member"}' >/dev/null
api "$AMINA" POST /api/projects/WEB/issues '{"title":"Add a Swahili translation of the home page"}' >/dev/null
api "$AMINA" POST /api/projects/WEB/issues '{"title":"The footer link to the code is broken"}' >/dev/null
api "$FOUNDER" DELETE /api/projects/WEB/members/amina >/dev/null
api "$FOUNDER" POST /api/projects/WEB/issues '{"title":"Pick a font"}' >/dev/null
api "$FOUNDER" PUT "/api/issues/WEB-1/labels/${WEB_BUG}" >/dev/null
api "$FOUNDER" PUT "/api/issues/WEB-2/labels/${WEB_BUG}" >/dev/null
api "$FOUNDER" POST /api/projects/SEC/issues '{"title":"Session tokens outlive a password change"}' >/dev/null
api "$FOUNDER" PUT "/api/issues/SEC-1/labels/${SEC_BUG}" >/dev/null
note "WEB-1 and WEB-2 are bugs; amina wrote WEB-4 and WEB-5 while a member, and is not one now"
note "SEC is private, and SEC-1 is a bug amina cannot see"
check "amina cannot see SEC at all" test "$(status "$AMINA" GET /api/projects/SEC)" = 404

say "In the browser: share two views, open them as amina, and label a mixed batch"
browser projects-milestone-6 | tee "$WORK/browser.log"

say "From outside the browser"
VIEWS="$(api "$AMINA" GET /api/views)"
EVERY="$(echo "$VIEWS" | json '[v["id"] for v in d if v["name"]=="Every bug"][0]')"
check "the shared search is listed for amina, as the founder's" \
  test "$(echo "$VIEWS" | json '[v["owner"] for v in d if v["name"]=="Every bug"][0]')" = founder
check "it is stored as its question — a label by name — and nothing it found" \
  test "$(api "$FOUNDER" GET "/api/views/${EVERY}" | json 'json.dumps(d["filter"], sort_keys=True)')" = '{"label": "bug"}'
check "opened by the founder it finds three bugs, SEC-1 among them" \
  test "$(api "$FOUNDER" GET "/api/views/${EVERY}/results" | json '",".join(sorted(i["key"] for i in d["issues"]))')" = "SEC-1,WEB-1,WEB-2"
check "opened by amina it finds the two she may see" \
  test "$(api "$AMINA" GET "/api/views/${EVERY}/results" | json '",".join(sorted(i["key"] for i in d["issues"]))')" = "WEB-1,WEB-2"
BOARD="$(api "$AMINA" GET /api/projects/WEB/views | json '[v["id"] for v in d if v["name"]=="Bugs on the board"][0]')"
check "the shared board view, opened by amina, is the board's two bugs" \
  test "$(api "$AMINA" GET "/api/views/${BOARD}/results" | json 'sum(len(c["cards"]) for c in d["board"]["columns"])')" = 2
check "amina cannot change the founder's view" \
  test "$(status "$AMINA" PATCH "/api/views/${EVERY}" '{"name":"Mine now"}')" = 403

for ref in WEB-4 WEB-5; do
  check "${ref} was labelled triage, and its own history records it" \
    test "$(api "$FOUNDER" GET "/api/issues/${ref}/history" | json '[e["type"] for e in d][-1]')" = issue.labeled
done
for ref in WEB-1 WEB-6; do
  check "${ref} was refused, and nothing was recorded against it" \
    test "$(api "$FOUNDER" GET "/api/issues/${ref}/history" | json 'sum(1 for e in d if e["type"]=="issue.labeled" and e["content"].get("labelName")=="triage")')" = 0
done

say "Projects Milestone 6 demonstrated"
note "a filtered board and a search across projects were saved, shared and opened by somebody else"
note "each showed amina only what she may see, though the founder sees more through the same view"
note "a mixed batch changed what it could, refused the rest by name, and recorded each change on its own issue"
