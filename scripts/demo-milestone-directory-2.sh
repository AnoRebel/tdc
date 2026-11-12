#!/usr/bin/env bash
#
# The directory's second milestone: skills, availability, and being found.
#
# A developer lists three skills — one of them new, which enters review rather
# than search — marks themselves available, and a client finds them by skill
# and availability. Hiding the profile takes it out of that same search at
# once. Member search, which the directory must not touch, answers exactly as
# it did before the directory was switched on. Then the same in a browser at
# phone width.
#
# Usage: scripts/demo-milestone-directory-2.sh
#        TDC_DEMO_SKIP_GENERATE=1 to reuse an existing web build.
set -euo pipefail

DEMO_NAME=m2
DEMO_PORT="${TDC_DEMO_PORT:-8302}"
# shellcheck source=scripts/demo-directory-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-directory-common.sh"

build
start

api POST /api/setup "" "{\"username\":\"founder\",\"password\":\"$PASSWORD\"}" >/dev/null
FOUNDER="$(signin founder)"
NEEMA="$(register neema)"
CLIENT="$(register baraka)"
PLAIN="$(register jumaplain)"

say "Member search, before the directory exists"
api GET "/api/search/everything?q=jumaplain" "$CLIENT" >/dev/null
PEOPLE_BEFORE="$(json 'json.dumps(d["people"], sort_keys=True)')"
note "$PEOPLE_BEFORE"

api PUT /api/admin/features/directory "$FOUNDER" '{"enabled":true}' >/dev/null
expect_status 204 "the operator turns the directory on"

say "Neema lists three skills, one of them new"
api POST /api/directory/skills "$NEEMA" '{"name":"Golang"}' >/dev/null
expect_status 200 "'Golang' is an existing skill under another spelling, not a new entry"
check 'd["name"] == "Go" and d["status"] == "active"' "it resolves to Go"
api POST /api/directory/skills "$NEEMA" '{"name":"Odoo ERP"}' >/dev/null
expect_status 201 "'Odoo ERP' is not in the vocabulary, so it is proposed"
check 'd["status"] == "proposed"' "and waits for review"
ODOO="$(json 'd["id"]')"

UNTIL="$(python3 -c 'import datetime; print((datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
api PUT /api/directory/me "$NEEMA" "{
  \"headline\": \"Backend and ERP developer\",
  \"location\": \"Dodoma\",
  \"visibility\": \"members\",
  \"skills\": [{\"name\": \"Go\"}, {\"name\": \"PostgreSQL\", \"description\": \"six years\"}, {\"id\": \"$ODOO\"}],
  \"openToWork\": true,
  \"availableUntil\": \"$UNTIL\",
  \"availabilityNote\": \"Three-month contracts\"
}" >/dev/null
expect_status 201 "the profile is saved with three skills and availability until $UNTIL"
check '[s["status"] for s in d["profile"]["skills"]] == ["active", "active", "proposed"]' \
  "neema sees two skills in the vocabulary and one waiting for review"

api GET /api/directory/moderation/skills "$FOUNDER" >/dev/null
check 'any(s["name"] == "Odoo ERP" for s in d)' "the proposal is in the review queue"

say "Neema's presence is hidden; her availability is not about presence"
api PUT /api/me/privacy "$NEEMA" '{"showsPresence":false}' >/dev/null
expect_status 204 "neema hides whether she is online"

say "The client searches by skill and availability"
api GET "/api/directory/search?skill=postgres&available=true" "$CLIENT" >/dev/null
check '[p["username"] for p in d["profiles"]] == ["neema"]' "searching postgres (another spelling) and available finds neema"
check 'd["profiles"][0]["available"] and d["profiles"][0]["availabilityNote"] == "Three-month contracts"' \
  "shown as available, with the kind of work she wants"
check '"Odoo ERP" not in d["profiles"][0]["skills"]' "without the skill still in review"
api GET "/api/directory/search?skill=Odoo%20ERP" "$CLIENT" >/dev/null
check 'd["profiles"] == []' "the proposed skill finds nobody until it is approved"
api GET "/api/directory/search?limit=1000&pageSize=1000" "$CLIENT" >/dev/null
check 'd["pageSize"] == 20' "a client asking for a thousand results is given one page of twenty at most"

say "Hiding the profile removes it from the same search"
api PUT /api/directory/me "$NEEMA" "{
  \"headline\": \"Backend and ERP developer\", \"location\": \"Dodoma\", \"visibility\": \"hidden\",
  \"skills\": [{\"name\": \"Go\"}, {\"name\": \"PostgreSQL\"}, {\"id\": \"$ODOO\"}],
  \"openToWork\": true, \"availableUntil\": \"$UNTIL\"
}" >/dev/null
expect_status 200 "neema hides her profile"
api GET "/api/directory/search?skill=postgres&available=true" "$CLIENT" >/dev/null
check 'd["profiles"] == []' "the same search now finds nobody"
api GET /api/directory/profiles/neema "$CLIENT" >/dev/null
expect_status 404 "and her profile answers as if it did not exist"

say "Member search is exactly what it was"
api GET "/api/search/everything?q=jumaplain" "$CLIENT" >/dev/null
PEOPLE_AFTER="$(json 'json.dumps(d["people"], sort_keys=True)')"
[[ "$PEOPLE_BEFORE" == "$PEOPLE_AFTER" ]] \
  || fail "member search changed: $PEOPLE_BEFORE -> $PEOPLE_AFTER"
pass "member search for a person outside the directory returns the same bytes as before it existed"
api GET "/api/search/everything?q=neema" "$CLIENT" >/dev/null
check 'sorted(d["people"][0].keys()) == ["address", "displayName", "id", "username"]' \
  "and describes somebody with a directory profile with the same four fields as anybody"

say "The same, in a browser at phone width"
browser tests/directory-search.spec.ts

say "Milestone demonstrated"
