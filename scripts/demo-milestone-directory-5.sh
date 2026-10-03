#!/usr/bin/env bash
#
# The directory's fifth milestone: the engagement and its private conversation.
#
# An accepted enquiry becomes an engagement with a private conversation; both
# parties see the same state, recorded step by step; one withdraws before any
# proposal and the engagement closes as withdrawn with no agreement, visible to
# both. Then the same in two browsers at once, watching the change arrive live.
#
# Usage: scripts/demo-milestone-directory-5.sh
#        TDC_DEMO_SKIP_GENERATE=1 to reuse an existing web build.
set -euo pipefail

DEMO_NAME=m5
DEMO_PORT="${TDC_DEMO_PORT:-8305}"
# shellcheck source=scripts/demo-directory-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-directory-common.sh"

build
start
founder_with_directory

NEEMA="$(register neema)"
CLIENT="$(register baraka)"
OTHER="$(register other)"
api PUT /api/directory/me "$NEEMA" '{"visibility":"public","headline":"Backend developer"}' >/dev/null
expect_status 201 "neema has a public profile"

say "An accepted enquiry becomes an engagement"
api POST /api/directory/profiles/neema/enquiries "$CLIENT" \
  '{"subject":"Payroll export","body":"An export from our payroll system to the tax portal."}' >/dev/null
expect_status 201 "baraka sends neema an enquiry"
ENQUIRY="$(json 'd["id"]')"
api POST "/api/directory/enquiries/$ENQUIRY/accept" "$NEEMA" >/dev/null
expect_status 201 "neema takes it up"
ENGAGEMENT="$(json 'd["id"]')"
ROOM="$(json 'd["roomId"]')"

for who in NEEMA CLIENT; do
  api GET "/api/directory/engagements/$ENGAGEMENT" "${!who}" >/dev/null
  check 'd["state"] == "discussion"' "${who,,} sees the engagement in discussion"
done
check 'd["transitions"][0]["from"] == "enquiry" and d["transitions"][0]["actor"] == "neema" and d["transitions"][0]["at"]' \
  "the step is recorded with who and when: $(json 'd["transitions"][0]')"

say "The conversation is private to the two of them"
api POST "/api/rooms/$ROOM/messages" "$CLIENT" '{"body":"zanzibarpineapple: thank you for taking this up"}' >/dev/null
expect_status 201 "baraka can now talk in the engagement's conversation"
api POST "/api/rooms/$ROOM/messages" "$NEEMA" '{"body":"Happy to. Send me the export format."}' >/dev/null
expect_status 201 "and so can neema"
api GET "/api/rooms/$ROOM/timeline" "$OTHER" >/dev/null
[[ "$(status)" != "200" ]] || fail "somebody outside the engagement read its conversation"
pass "another member cannot read it ($(status))"
api GET "/api/directory/engagements/$ENGAGEMENT" "$FOUNDER" >/dev/null
expect_status 404 "the server's administrator is told there is no such engagement"
for who in OTHER FOUNDER; do
  api GET "/api/search/everything?q=zanzibarpineapple" "${!who}" >/dev/null
  check '"zanzibarpineapple" not in json.dumps(d)' "${who,,}'s search does not find what was said in it"
done
api GET "/api/search/everything?q=zanzibarpineapple" "$NEEMA" >/dev/null
check '"zanzibarpineapple" in json.dumps(d)' "while neema's own search does, so the search works"

say "Baraka withdraws before any proposal"
api POST "/api/directory/engagements/$ENGAGEMENT/withdraw" "$CLIENT" '{"reason":"We found somebody in town"}' >/dev/null
expect_status 200 "baraka withdraws"
for who in NEEMA CLIENT; do
  api GET "/api/directory/engagements/$ENGAGEMENT" "${!who}" >/dev/null
  check 'd["state"] == "withdrawn" and "agreement" not in d' "${who,,} sees it withdrawn, with no agreement"
done
check 'd["transitions"][-1]["actor"] == "baraka" and d["transitions"][-1]["note"] == "We found somebody in town"' \
  "the withdrawal is recorded with who and why"
api POST "/api/rooms/$ROOM/messages" "$NEEMA" '{"body":"One more thing"}' >/dev/null
[[ "$(status)" != "201" ]] || fail "the conversation of a withdrawn engagement still takes messages"
pass "its conversation is kept and closed to new messages ($(status))"

say "The same in two browsers at once, watching it change live"
browser tests/directory-engagement.spec.ts

say "Milestone demonstrated"
