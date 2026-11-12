#!/usr/bin/env bash
#
# Events Milestone 2: invitations, answers and reminders.
#
# hana, a member, schedules a twenty-place, invite-only workshop in #workshops
# in the browser, and invites the whole channel from the event's page. Nineteen
# people answer going through the API and baraka takes the twentieth place in
# his browser; zuri, the twenty-first, is put on the waiting list. p07 declines,
# and zuri moves up and is told. The workshop starts a day and three minutes
# after it is scheduled, so the server's default day-before reminder falls due
# during the demonstration: zuri sees it arrive on the page and in her
# notifications, and every person going — and nobody who declined — finds it
# in the notification feed of a second session, which is what the desktop
# application reads.
#
# Usage: scripts/demo-milestone-events-2.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m2
DEMO_PORT="${TDC_DEMO_PORT:-8502}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-events-lib.sh"

build
start_server
check "reminders are sent a day and a quarter of an hour before, as the hosting guide says" \
  test "$(api "$FOUNDER" GET /api/admin/settings | json '[s for s in (d if isinstance(d,list) else d["settings"]) if s["key"]=="events.reminder_minutes"][0]["value"]')" = "1440,15"

say "Twenty-two members of #workshops: hana hosts, baraka and zuri use browsers, p01–p19 the API"
CHANNEL="$(channel workshops)"
for who in hana baraka zuri $(seq -f 'p%02g' 1 19); do
  person "$who" "${who^}"
  api "$(token_of "$who")" POST "/api/rooms/${CHANNEL}/join" >/dev/null
done
note "made ${PEOPLE_MADE} people, each batch of four from its own loopback address"

say "In the browser: the workshop, its invitation, its waiting list and its reminder"
browser events-milestone-2

say "From outside the browser: the answers the server holds"
EVENT="$(api "$(token_of hana)" GET /api/events | json '[e for e in d if e["title"]=="Intro to Go workshop"][0]["id"]')"
COUNTS="$(api "$(token_of hana)" GET "/api/events/${EVENT}")"
check "twenty are going, which is every place" \
  test "$(echo "$COUNTS" | json 'd["counts"]["going"]')" = "20"
check "nobody is left waiting" \
  test "$(echo "$COUNTS" | json 'd["counts"].get("waitlisted", 0)')" = "0"
check "zuri is going" \
  test "$(api "$(token_of zuri)" GET "/api/events/${EVENT}" | json 'd["myAnswer"]')" = "going"
check "the day-before reminder was claimed once, for the one occurrence" \
  test "$(psql "${DATABASE_SERVER}/${DATABASE}" -tAq \
    -c "SELECT count(*) || ':' || min(lead_minutes) FROM calendar_reminders WHERE event_id = '${EVENT}'")" = "1:1440"

say "Events Milestone 2 demonstrated"
note "hana scheduled a twenty-place workshop and invited #workshops from its page; before that, #workshops could not see it"
note "twenty took the places; zuri, the twenty-first, was told she was number 1 on the waiting list"
note "p07 declined; zuri moved up to going and was told a place had freed"
note "a day before it starts, zuri saw the reminder arrive on the page and in her notifications"
note "every one of the twenty going, and the host, had it in the notification feed of a second session —"
note "  the feed the desktop application reads; the desktop itself was not run here. p07 had none"
