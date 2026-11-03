#!/usr/bin/env bash
#
# Events Milestone 1: events exist.
#
# In real browsers, each in its own time zone: amina, in Dar es Salaam,
# schedules a weekly meetup in #dar-devs at 18:00 her time; james, in London,
# sees every occurrence at his own time with hers beside it — and sees it move
# an hour when London changes to summer time and Dar es Salaam does not; otto,
# who is not in the channel, finds it neither in his events, nor at its own
# address, nor through the API or search.
#
# Usage: scripts/demo-milestone-events-1.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m1
DEMO_PORT="${TDC_DEMO_PORT:-8501}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-events-lib.sh"

build
start_server

say "Three members; amina and james are in #dar-devs, otto is not"
person amina "Amina"
person james "James"
person otto "Otto"
CHANNEL="$(channel dar-devs)"
for who in amina james; do
  api "$(token_of "$who")" POST "/api/rooms/${CHANNEL}/join" >/dev/null
done
note "made amina, james and otto; #dar-devs has amina and james"

say "In the browser: scheduling in Dar es Salaam, reading in London, looking from outside"
browser events-milestone-1

say "From outside the browser: what was stored is one instant and the zone it was meant in"
EVENT="$(api "$(token_of amina)" GET /api/events | json '[e for e in d if e["title"]=="Dar Devs weekly meetup"][0]["id"]')"
STORED="$(api "$(token_of amina)" GET "/api/events/${EVENT}")"
check "it is kept in Africa/Dar_es_Salaam" \
  test "$(echo "$STORED" | json 'd["timeZone"]')" = "Africa/Dar_es_Salaam"
check "it starts at 15:00 UTC, which is 18:00 in Dar es Salaam" \
  test "$(echo "$STORED" | json 'd["startsAt"][11:16]')" = "15:00"
check "it repeats weekly, on Thursdays" \
  grep -q "FREQ=WEEKLY.*BYDAY=TH\|BYDAY=TH.*FREQ=WEEKLY" <<<"$(echo "$STORED" | json 'd["recurrence"]')"
check "it belongs to #dar-devs and its members" \
  test "$(echo "$STORED" | json 'd["visibility"]')" = "channel"

say "Events Milestone 1 demonstrated"
note "amina scheduled a weekly meetup in #dar-devs at 18:00 Dar es Salaam time, in the browser"
note "james, in London, saw it at 15:00 his time with 'Thu 18:00 for the organiser' beside it,"
note "  and at 16:00 once London is on summer time — the organiser's 18:00 never moved"
note "otto, outside the channel, did not find it in his events, at its address, in search or the API"
