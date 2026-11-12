#!/usr/bin/env bash
#
# Events Milestone 5: calendars in.
#
# amina, in Dar es Salaam, imports a file exported from Google Calendar
# (apps/web/milestones/fixtures/google-calendar-export.ics): a weekly study
# group at 18:00 Dar es Salaam time with one week skipped, a call at 19:00
# London time, and an all-day sprint. The preview says what each becomes, and
# they arrive at the right times: every Tuesday but the skipped one at 18:00, the
# call at 22:00 her time with London's beside it. Importing the file again would
# update, not duplicate.
#
# Then she follows a community's calendar, served from a plain HTTP server on
# this machine. The community adds an event; after a refresh it is here.
#
# The operator's part comes from docs/hosting.md: following calendars takes
# event.calendars.manage, given here through a role, and since the community's
# server is on this machine, integrations.allow_private_addresses is turned on —
# without it the server refuses the address, as it should.
#
# Usage: scripts/demo-milestone-events-5.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m5
DEMO_PORT="${TDC_DEMO_PORT:-8505}"
CALENDAR_PORT="${TDC_DEMO_CALENDAR_PORT:-8509}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-events-lib.sh"

build
start_server

say "amina is in #python and may follow calendars"
person amina "Amina"
CHANNEL="$(channel python)"
api "$(token_of amina)" POST "/api/rooms/${CHANNEL}/join" >/dev/null
ROLE="$(api "$FOUNDER" POST /api/admin/roles '{"name":"Calendar keepers","description":"People who follow calendars for the community"}' | json 'd["id"]')"
api "$FOUNDER" PUT "/api/admin/roles/${ROLE}/permissions" '{"permission":"event.calendars.manage","granted":true}' >/dev/null
api "$FOUNDER" POST "/api/admin/roles/${ROLE}/members" "{\"user\":\"$(id_of amina)\"}" >/dev/null
note "a Calendar keepers role holds event.calendars.manage; amina is in it"

say "The community's calendar, served from this machine"
COMMUNITY_DIR="$WORK/community"
mkdir -p "$COMMUNITY_DIR"
cp "$ROOT/apps/web/milestones/fixtures/community-calendar.ics" "$COMMUNITY_DIR/community.ics"
python3 -m http.server "$CALENDAR_PORT" --bind 127.0.0.1 --directory "$COMMUNITY_DIR" >"$WORK/calendar.log" 2>&1 &
EXTRA_PIDS+=($!)
COMMUNITY_URL="http://127.0.0.1:${CALENDAR_PORT}/community.ics"
for _ in $(seq 1 30); do curl -sf "$COMMUNITY_URL" >/dev/null && break; sleep 0.2; done
check "a private address is refused while the operator has not allowed it" \
  test "$(status "$(token_of amina)" POST /api/calendar/subscriptions \
    "{\"url\":\"${COMMUNITY_URL}\",\"label\":\"refused\"}")" = "400"
setting integrations.allow_private_addresses true
note "integrations.allow_private_addresses on; the calendar is ${COMMUNITY_URL}"

say "In the browser: import the Google Calendar export, then follow and refresh the community's"
browser events-milestone-5 TDC_COMMUNITY_URL="$COMMUNITY_URL" TDC_COMMUNITY_DIR="$COMMUNITY_DIR"

say "From outside the browser: what was imported is what Google meant"
STUDY="$(api "$(token_of amina)" GET "/api/events?from=2027-01-01T00:00:00Z&to=2027-02-01T00:00:00Z" \
  | json '[e for e in d if e["title"]=="Python study group"][0]["id"]')"
STORED="$(api "$(token_of amina)" GET "/api/events/${STUDY}")"
check "the study group is kept in Africa/Dar_es_Salaam" \
  test "$(echo "$STORED" | json 'd["timeZone"]')" = "Africa/Dar_es_Salaam"
check "it starts at 15:00 UTC on 5 January 2027, which is 18:00 there" \
  test "$(echo "$STORED" | json 'd["startsAt"]')" = "2027-01-05T15:00:00Z"
check "it repeats weekly on Tuesdays" \
  test "$(echo "$STORED" | json 'd["recurrence"]')" = "FREQ=WEEKLY;BYDAY=TU"
check "with 19 January skipped" \
  test "$(echo "$STORED" | json '",".join(d["exceptions"])')" = "2027-01-19T15:00:00Z"
check "the community's calendar was fetched by the server, twice" \
  test "$(grep -c 'GET /community.ics' "$WORK/calendar.log")" -ge 2

say "Events Milestone 5 demonstrated"
note "a Google Calendar export was previewed and imported: the weekly study group at 18:00 every Tuesday,"
note "  the skipped week skipped, the London call at 22:00 Dar es Salaam time, the all-day sprint all day"
note "importing it again would have updated the same three events, not made three more"
note "the Nairobi Gophers' calendar was followed; its new event appeared here after Refresh now"
