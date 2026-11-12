#!/usr/bin/env bash
#
# Events Milestone 3: hosting.
#
# A talk hosted in a call here: neema, its host, starts its call from the
# talk's page; kofi and lulu answer going and join from theirs, with camera and
# microphone; grace, who has no account, joins through a link neema makes from
# the call window. Chromium runs with a fake camera and microphone, so the
# media is real.
#
# The operator's part comes from docs/hosting.md: calls on, guests allowed, and
# call.link given to the people who host — here through a role, as an operator
# would.
#
# Usage: scripts/demo-milestone-events-3.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m3
DEMO_PORT="${TDC_DEMO_PORT:-8503}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-events-lib.sh"

build
start_server

say "The operator turns calls on, allows guests, and lets hosts make call links"
feature calls true
setting calls.guests true
ROLE="$(api "$FOUNDER" POST /api/admin/roles '{"name":"Event hosts","description":"People who host talks"}' | json 'd["id"]')"
api "$FOUNDER" PUT "/api/admin/roles/${ROLE}/permissions" '{"permission":"call.link","granted":true}' >/dev/null
note "calls on, calls.guests on, an Event hosts role holding call.link"

say "neema hosts a talk in #talks, hosted in a call here; kofi and lulu are in the channel"
CHANNEL="$(channel talks)"
for who in neema kofi lulu; do
  person "$who" "${who^}"
  api "$(token_of "$who")" POST "/api/rooms/${CHANNEL}/join" >/dev/null
done
api "$FOUNDER" POST "/api/admin/roles/${ROLE}/members" "{\"user\":\"$(id_of neema)\"}" >/dev/null
read -r STARTS ENDS < <(python3 -c '
from datetime import datetime, timedelta, timezone
start = (datetime.now(timezone.utc) + timedelta(minutes=10)).replace(second=0, microsecond=0)
print(start.strftime("%Y-%m-%dT%H:%M:%SZ"), (start + timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))')
EVENT="$(api "$(token_of neema)" POST /api/events "{
  \"channelId\": \"${CHANNEL}\", \"kind\": \"talk\", \"title\": \"Generics in practice\",
  \"description\": \"A talk, then questions.\", \"startsAt\": \"${STARTS}\", \"endsAt\": \"${ENDS}\",
  \"timeZone\": \"Africa/Dar_es_Salaam\", \"online\": true, \"visibility\": \"channel\"}" | json 'd["id"]')"
note "the talk is ${BASE}/events/${EVENT}, starting ${STARTS}"

say "In the browser: the host starts the call, attendees join, a guest comes in by link"
browser events-milestone-3 TDC_EVENT_ID="$EVENT"

say "From outside the browser: the call was the talk's own"
CALLS="$(psql "${DATABASE_SERVER}/${DATABASE}" -tAq -c "SELECT count(*) || ':' || bool_and(ended_at IS NOT NULL) || ':' || bool_and(started_by = '$(id_of neema)') FROM calls WHERE room_id = '${EVENT}'")"
check "one call was held, in the talk's own conversation, started by neema, and it has ended ($CALLS)" \
  test "$CALLS" = "1:true:true"
check "an attendee may not start a talk's call" \
  test "$(api "$(token_of kofi)" GET "/api/events/${EVENT}" | json 'd["may"]["startCall"]')" = "False"
check "the host may" \
  test "$(api "$(token_of neema)" GET "/api/events/${EVENT}" | json 'd["may"]["startCall"]')" = "True"

say "Events Milestone 3 demonstrated"
note "neema started the talk's call from its page; before that kofi could neither start nor join it"
note "kofi and lulu answered going and joined from the talk's page; all three saw each other's cameras"
note "neema made a guest link from the call window; grace, with no account, opened it, waited in the"
note "  lobby, was let in by neema, and saw the others' cameras"
