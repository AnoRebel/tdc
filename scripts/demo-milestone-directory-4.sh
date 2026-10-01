#!/usr/bin/env bash
#
# The directory's fourth milestone: first contact.
#
# A client sends an enquiry to an available developer's profile; the developer
# is notified and reads it. A second developer has closed enquiries and the same
# attempt is refused with a plain message. A client sending enquiries in quick
# succession is rate limited. Then the same on a phone.
#
# Usage: scripts/demo-milestone-directory-4.sh
#        TDC_DEMO_SKIP_GENERATE=1 to reuse an existing web build.
set -euo pipefail

DEMO_NAME=m4
DEMO_PORT="${TDC_DEMO_PORT:-8304}"
# shellcheck source=scripts/demo-directory-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-directory-common.sh"

build
start
founder_with_directory

NEEMA="$(register neema)"
ZAWADI="$(register zawadi)"
CLIENT="$(register baraka)"
UNTIL="$(python3 -c 'import datetime; print((datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

api PUT /api/directory/me "$NEEMA" "{\"visibility\":\"public\",\"headline\":\"Mobile developer\",
  \"openToWork\":true,\"availableUntil\":\"$UNTIL\"}" >/dev/null
expect_status 201 "neema has a public profile and is available"
api PUT /api/me/privacy "$NEEMA" '{"directMessages":"nobody"}' >/dev/null
expect_status 204 "neema takes direct messages from nobody"
api PUT /api/directory/me "$ZAWADI" '{"visibility":"public","headline":"Designer","acceptsEnquiries":false}' >/dev/null
expect_status 201 "zawadi has a public profile and has closed enquiries"

say "The client sends neema an enquiry"
api GET /api/directory/profiles/neema "$CLIENT" >/dev/null
check 'd["acceptsEnquiries"] is True and d["available"] is True' "her profile says she is available and takes enquiries"
api POST /api/directory/profiles/neema/enquiries "$CLIENT" \
  '{"subject":"Clinic booking app","body":"A booking app for three clinics in Mwanza, by March."}' >/dev/null
expect_status 201 "the enquiry is sent, although neema takes no direct messages"
ROOM="$(json 'd["roomId"]')"

api GET /api/me/notifications "$NEEMA" >/dev/null
check "len(d) == 1 and d[0]['roomId'] == '$ROOM' and d[0]['reason'] == 'direct'" "neema is notified, as for any message addressed to her"
api GET /api/me/notifications "$CLIENT" >/dev/null
check 'len(d) == 0' "the client is not notified of their own enquiry"
api GET /api/directory/me/enquiries "$NEEMA" >/dev/null
check 'len(d) == 1 and d[0]["subject"] == "Clinic booking app" and d[0]["with"] == "baraka"' "it is in neema's enquiries"
api GET "/api/rooms/$ROOM/timeline" "$NEEMA" >/dev/null
check 'any("three clinics in Mwanza" in json.dumps(e) for e in d)' "and she reads it in its conversation"
api POST "/api/rooms/$ROOM/messages" "$CLIENT" '{"body":"Are you there?"}' >/dev/null
[[ "$(status)" != "201" ]] || fail "the client added to an enquiry nobody has answered"
pass "the client cannot add to it until neema answers ($(status))"
api POST /api/direct "$CLIENT" "{\"userIds\":[\"$(api GET /api/me "$NEEMA" >/dev/null; json 'd["id"]')\"]}" >/dev/null
[[ "$(status)" != "201" && "$(status)" != "200" ]] || fail "a direct message to neema was opened despite her setting"
pass "and direct messages to neema stay closed: the two settings are separate ($(status))"

say "Zawadi has closed enquiries"
api GET /api/directory/profiles/zawadi "$CLIENT" >/dev/null
check 'd["acceptsEnquiries"] is False' "her profile says she takes none"
api POST /api/directory/profiles/zawadi/enquiries "$CLIENT" \
  '{"subject":"Clinic booking app","body":"A booking app for three clinics in Mwanza, by March."}' >/dev/null
expect_status 403 "the same enquiry to zawadi is refused"
check '"not accepting enquiries" in d["message"]' "with a plain message: $(json 'd["message"]')"
api GET /api/directory/me/enquiries "$ZAWADI" >/dev/null
check 'd == []' "and nothing reached her"

say "A client approaching everybody at once is rate limited"
# Five registrations an hour from one address is the server's own limit, so the
# people approached are ones already here: neema, one more, and the founder.
api PUT /api/admin/settings/directory.enquiries_per_hour "$FOUNDER" '{"value":"2"}' >/dev/null
expect_status 204 "the operator allows two enquiries an hour"
BULK="$(register bulk)"
ONE="$(register one)"
api PUT /api/directory/me "$ONE" '{"visibility":"public"}' >/dev/null
api PUT /api/directory/me "$FOUNDER" '{"visibility":"public"}' >/dev/null
for name in neema one; do
  api POST "/api/directory/profiles/$name/enquiries" "$BULK" '{"subject":"Cheap work","body":"Same message to everybody."}' >/dev/null
  expect_status 201 "an enquiry to $name"
done
api POST /api/directory/profiles/founder/enquiries "$BULK" '{"subject":"Cheap work","body":"Same message to everybody."}' >/dev/null
expect_status 429 "the third in the hour is refused"
api GET /api/directory/me/enquiries "$FOUNDER" >/dev/null
check 'd == []' "and did not arrive"
api POST /api/directory/profiles/founder/enquiries "$CLIENT" '{"subject":"Real work","body":"A considered enquiry."}' >/dev/null
expect_status 201 "while another client's ordinary enquiry to the same person goes through"

say "The same on a phone"
browser tests/directory-enquiry.spec.ts

say "Milestone demonstrated"
