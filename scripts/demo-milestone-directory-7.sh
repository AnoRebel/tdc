#!/usr/bin/env bash
#
# The directory's seventh milestone: switched on and off on a running server.
#
# A server that has been running chat has no directory. Its operator switches
# the directory on with the command the hosting guide gives, without restarting
# anything. Two people who have never used it — a developer and a client —
# follow the user guide in two phone browsers: each registers, the developer
# makes a profile, the client finds it and sends an enquiry, it is taken up,
# proposed, accepted by both and exported. The operator then switches the
# directory off: every directory route answers 404 and the interface has no
# way in, while chat carries on and the directory's data is all still there,
# coming back unchanged when it is switched on again. One server process
# throughout.
#
# Usage: scripts/demo-milestone-directory-7.sh
#        TDC_DEMO_SKIP_GENERATE=1 to reuse an existing web build.
set -euo pipefail

DEMO_NAME=m7
DEMO_PORT="${TDC_DEMO_PORT:-8307}"
# shellcheck source=scripts/demo-directory-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-directory-common.sh"

# The browser half's people, named for this demonstration.
export TDC_DIRECTORY_RUN="m7$(date +%s | tail -c 6)"
DEVELOPER="amani$TDC_DIRECTORY_RUN"
CLIENT_NAME="baraka$TDC_DIRECTORY_RUN"

# Against this server as it runs: no restart, which is the claim.
in_browser() {
  (cd "$ROOT/apps/web" && TDC_TEST_URL="$BASE" bunx playwright test --project=directory "$@") \
    || fail "the browser checks did not pass"
}
still_running() {
  kill -0 "$SERVER_PID" 2>/dev/null || fail "the server stopped"
  pass "the same server process is still running (pid $SERVER_PID)"
}

build
start

say "A server running chat, with no directory"
api POST /api/setup "" "{\"username\":\"founder\",\"password\":\"$PASSWORD\"}" >/dev/null
FOUNDER="$(signin founder)"
api POST /api/rooms "$FOUNDER" '{"slug":"general","name":"General","topic":"Everyday chat"}' >/dev/null
expect_status 201 "the founder opens a channel"
GENERAL="$(json 'd["id"]')"
api POST "/api/rooms/$GENERAL/messages" "$FOUNDER" '{"body":"Karibu, everyone"}' >/dev/null
expect_status 201 "and says hello in it"
api GET /api/server "" >/dev/null
check '"directory" not in d["capabilities"]' "the server does not offer a directory"
api GET /api/directory/search "$FOUNDER" >/dev/null
expect_status 404 "and its routes answer as though there were none"

say "The operator switches it on, as the hosting guide says"
curl -s -o /dev/null -w '%{http_code}' -X PUT "$BASE/api/admin/features/directory" \
  -H "Authorization: Bearer $FOUNDER" -H 'Content-Type: application/json' \
  -d '{"enabled": true}' >"$WORK/status"
expect_status 204 "PUT /api/admin/features/directory {\"enabled\": true}"
still_running
api GET /api/server "" >/dev/null
check '"directory" in d["capabilities"]' "the server now offers the directory"

say "Two new people follow the user guide on their phones"
note "$DEVELOPER (developer) and $CLIENT_NAME (client): register, profile, find, enquire, propose, accept, export"
in_browser tests/directory-cycle.spec.ts
still_running

DEV="$(signin "$DEVELOPER")"
BUYER="$(signin "$CLIENT_NAME")"
api GET /api/directory/me/engagements "$DEV" >/dev/null
check 'len(d) == 1 and d[0]["state"] == "agreement"' "the developer has one engagement, agreed"
ENGAGEMENT="$(json 'd[0]["id"]')"
api GET "/api/directory/engagements/$ENGAGEMENT/proposals" "$BUYER" >/dev/null
check 'd["agreement"]["version"] == 1 and d["agreement"]["terms"]["totalMinor"] == 80000000' \
  "the client sees the recorded agreement: $(json 'd["agreement"]["terms"]["currency"]') $(json 'd["agreement"]["terms"]["totalMinor"]') minor units"
api GET /api/directory/me "$DEV" >"$WORK/profile-before.json"
api GET "/api/directory/engagements/$ENGAGEMENT/proposals" "$DEV" >"$WORK/agreement-before.json"

say "Switched off, the interface has no way in (and it comes back)"
in_browser tests/directory-switched-off.spec.ts
still_running

say "The operator switches it off again"
curl -s -o /dev/null -w '%{http_code}' -X PUT "$BASE/api/admin/features/directory" \
  -H "Authorization: Bearer $FOUNDER" -H 'Content-Type: application/json' \
  -d '{"enabled": false}' >"$WORK/status"
expect_status 204 "PUT /api/admin/features/directory {\"enabled\": false}"
api GET /api/server "" >/dev/null
check '"directory" not in d["capabilities"]' "the server no longer offers it"

# Every route the server describes under /api/directory, called as the
# developer it belongs to, the founder, and nobody.
api GET /api/description "$FOUNDER" >/dev/null
python3 - "$WORK/body" >"$WORK/routes" <<'EOF'
import json, sys
for endpoint in json.load(open(sys.argv[1]))["endpoints"]:
    if endpoint["path"].startswith("/api/directory"):
        path = endpoint["path"]
        for parameter in endpoint.get("parameters") or []:
            path = path.replace(":" + parameter, "00000000-0000-0000-0000-000000000000")
        print(endpoint["method"], path)
EOF
ROUTES=0
while read -r method path; do
  for token in "$DEV" "$FOUNDER" ""; do
    api "$method" "$path" "$token" '{}' >/dev/null
    [[ "$(status)" == "404" ]] || fail "$method $path answered $(status) with the directory off"
  done
  ROUTES=$((ROUTES + 1))
done <"$WORK/routes"
[[ "$ROUTES" -gt 40 ]] || fail "only $ROUTES directory routes were described"
pass "all $ROUTES directory routes answer 404, to the developer, the founder and nobody"

say "Chat carries on, and nothing in the directory was deleted"
api POST "/api/rooms/$GENERAL/join" "$DEV" >/dev/null
expect_status 200 "the developer joins the channel"
api POST "/api/rooms/$GENERAL/messages" "$DEV" '{"body":"Still here"}' >/dev/null
expect_status 201 "and posts in it"
api GET "/api/rooms/$GENERAL/timeline" "$FOUNDER" >/dev/null
check '"Karibu, everyone" in json.dumps(d) and "Still here" in json.dumps(d)' "the channel has both messages"
COUNTS="$(docker exec tdc-database-1 psql -U tdc -d "$DATABASE" -tAc \
  "SELECT (SELECT count(*) FROM directory_profiles), (SELECT count(*) FROM engagements), (SELECT count(*) FROM engagement_agreements)")"
[[ "$COUNTS" == "1|1|1" ]] || fail "the directory's rows changed: profiles|engagements|agreements = $COUNTS"
pass "the profile, the engagement and the agreement are all stored ($COUNTS)"
api GET /api/directory/me/engagements "$DEV" >/dev/null
expect_status 404 "while none of it can be reached"
still_running

say "On again: everything back as it was"
api PUT /api/admin/features/directory "$FOUNDER" '{"enabled":true}' >/dev/null
expect_status 204 "the operator switches it back on"
api GET /api/directory/me "$DEV" >"$WORK/profile-after.json"
cmp -s "$WORK/profile-before.json" "$WORK/profile-after.json" || fail "the profile changed"
pass "the profile is exactly as it was"
api GET "/api/directory/engagements/$ENGAGEMENT/proposals" "$DEV" >"$WORK/agreement-after.json"
cmp -s "$WORK/agreement-before.json" "$WORK/agreement-after.json" || fail "the agreement changed"
pass "and so is the agreement"
still_running

say "Milestone demonstrated"
