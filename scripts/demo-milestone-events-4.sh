#!/usr/bin/env bash
#
# Events Milestone 4: calendars out.
#
# james, in London, is in #dar-devs, which meets weekly at 18:00 Dar es Salaam
# time and has a one-off talk at 19:00 London time. In the browser he makes his
# calendar address in Settings and copies it, as the user guide says. Then what
# a calendar application does with it is done here: the address is fetched
# with no session, read by Python's icalendar library — an implementation that
# is not this server's — and every occurrence put at its local time in London
# and in Dar es Salaam. The meetup is at 15:00 in London through the winter and
# 16:00 once London is on summer time, and at 18:00 in Dar es Salaam throughout;
# the talk is at 19:00 in London and 22:00 in Dar es Salaam. An event in a
# channel james is not in is not in his feed.
#
# No calendar application with a window is run: Google, Outlook and Apple each
# fetch the same address on their own schedule, and that is not something a
# script on this machine can drive. The parser stands in for their reading of it.
#
# Usage: scripts/demo-milestone-events-4.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m4
DEMO_PORT="${TDC_DEMO_PORT:-8504}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-events-lib.sh"

python3 -c 'import icalendar, dateutil' 2>/dev/null \
  || fail "this demonstration reads the feed with Python's icalendar (pip install icalendar)"

build
start_server

say "#dar-devs has a weekly meetup and a one-off talk; #elsewhere has an event james cannot see"
person amina "Amina"
person james "James"
DAR_DEVS="$(channel dar-devs)"
ELSEWHERE="$(channel elsewhere)"
for who in amina james; do api "$(token_of "$who")" POST "/api/rooms/${DAR_DEVS}/join" >/dev/null; done
api "$(token_of amina)" POST "/api/rooms/${ELSEWHERE}/join" >/dev/null

# The next Thursday at least two days away, and five weeks after it.
read -r THURSDAY TALK_DAY < <(python3 -c '
from datetime import date, timedelta
day = date.today() + timedelta(days=2)
while day.weekday() != 3: day += timedelta(days=1)
print(day, day + timedelta(weeks=5))')
schedule() {
  api "$(token_of amina)" POST /api/events "$1" | json 'd["id"]' >/dev/null
}
# 18:00 in Dar es Salaam is 15:00 UTC; 19:00 in London in December is 19:00 UTC.
schedule "{\"channelId\":\"${DAR_DEVS}\",\"kind\":\"meetup\",\"title\":\"Dar Devs weekly meetup\",
  \"startsAt\":\"${THURSDAY}T15:00:00Z\",\"endsAt\":\"${THURSDAY}T17:00:00Z\",
  \"timeZone\":\"Africa/Dar_es_Salaam\",\"recurrence\":\"FREQ=WEEKLY;BYDAY=TH\",
  \"location\":\"Kijitonyama, Dar es Salaam\",\"visibility\":\"channel\"}"
schedule "{\"channelId\":\"${DAR_DEVS}\",\"kind\":\"talk\",\"title\":\"Go and WebAssembly\",
  \"startsAt\":\"${TALK_DAY}T19:00:00Z\",\"endsAt\":\"${TALK_DAY}T20:00:00Z\",
  \"timeZone\":\"Europe/London\",\"online\":true,\"visibility\":\"channel\"}"
schedule "{\"channelId\":\"${ELSEWHERE}\",\"kind\":\"other\",\"title\":\"Elsewhere social\",
  \"startsAt\":\"${THURSDAY}T16:00:00Z\",\"endsAt\":\"${THURSDAY}T18:00:00Z\",
  \"timeZone\":\"Africa/Dar_es_Salaam\",\"visibility\":\"channel\"}"
note "a weekly meetup from ${THURSDAY}, a talk on ${TALK_DAY}, and an event in #elsewhere"

say "In the browser: james makes his calendar address, and it is fetched as an application would"
FEED="$WORK/feed.ics"
browser events-milestone-4 TDC_FEED_OUT="$FEED"

say "A second implementation reads the feed and puts each event at its local time"
python3 -I - "$FEED" "$THURSDAY" "$TALK_DAY" <<'PY'
import sys
from itertools import islice
from datetime import date, datetime, timedelta
from zoneinfo import ZoneInfo

import icalendar
from dateutil.rrule import rrulestr

path, thursday, talk_day = sys.argv[1], date.fromisoformat(sys.argv[2]), date.fromisoformat(sys.argv[3])
london, dar = ZoneInfo("Europe/London"), ZoneInfo("Africa/Dar_es_Salaam")
calendar = icalendar.Calendar.from_ical(open(path, "rb").read())
events = {str(e["SUMMARY"]): e for e in calendar.walk("VEVENT")}
zones = {str(z["TZID"]) for z in calendar.walk("VTIMEZONE")}

def ok(claim, holds):
    print(("  \033[32mok\033[0m  " if holds else "  \033[31mFAILED\033[0m  ") + claim)
    if not holds:
        sys.exit(1)

ok("the feed holds the meetup and the talk, and not #elsewhere's event",
   set(events) == {"Dar Devs weekly meetup", "Go and WebAssembly"})

meetup = events["Dar Devs weekly meetup"]
start = meetup.decoded("DTSTART")
ok("the meetup is written in its own zone, with that zone described in the file",
   str(meetup["DTSTART"].params.get("TZID")) == "Africa/Dar_es_Salaam" and "Africa/Dar_es_Salaam" in zones)
rule = meetup["RRULE"].to_ical().decode()
occurrences = list(islice(rrulestr(rule, dtstart=start), 26))
ok(f"it repeats ({rule}), every occurrence on a Thursday at 18:00 in Dar es Salaam",
   all(o.astimezone(dar).strftime("%a %H:%M") == "Thu 18:00" for o in occurrences))
first = occurrences[0].astimezone(london)
ok(f"its first occurrence is {first:%a %d %b} at 15:00 in London",
   first.date() == thursday and first.strftime("%H:%M") == "15:00")
summer = [o.astimezone(london) for o in occurrences if o.astimezone(london).dst()]
ok(f"once London is on summer time ({summer[0]:%d %b} on), it is at 16:00 there",
   bool(summer) and all(o.strftime("%H:%M") == "16:00" for o in summer))
winter = [o.astimezone(london) for o in occurrences if not o.astimezone(london).dst()]
ok("and at 15:00 in every winter week", all(o.strftime("%H:%M") == "15:00" for o in winter))

talk = events["Go and WebAssembly"]
at = talk.decoded("DTSTART")
ok(f"the talk is at 19:00 in London on {talk_day:%d %b}",
   at.astimezone(london).date() == talk_day and at.astimezone(london).strftime("%H:%M") == "19:00")
ok("which is 22:00 in Dar es Salaam", at.astimezone(dar).strftime("%H:%M") == "22:00")
ok("each has a URL back to its page", all(str(e.get("URL", "")).startswith("http") for e in events.values()))
PY

say "Events Milestone 4 demonstrated"
note "james made his calendar address in Settings and copied it; it was fetched with no session"
note "Python's icalendar read it: the weekly meetup at 15:00 in London (16:00 in summer), 18:00 in Dar es Salaam,"
note "  and the talk at 19:00 in London, 22:00 in Dar es Salaam; #elsewhere's event was not in it"
note "no calendar application with a window was run; the parser stands in for one reading the address"
