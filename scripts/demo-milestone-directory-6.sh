#!/usr/bin/env bash
#
# The directory's sixth milestone: proposals, the agreement, and finishing.
#
# A developer proposes; the client asks for a change to one line; the developer
# sends a revised version and the first stays as it was; one acceptance agrees
# nothing, and the agreement is recorded only when both accept the same version;
# the database refuses to rewrite it; both parties export it and get the same
# terms under the same statement the screen shows; one says the work is
# complete, and it stays outstanding until the other confirms. Then the whole
# cycle by two people in two browsers on phones.
#
# Usage: scripts/demo-milestone-directory-6.sh
#        TDC_DEMO_SKIP_GENERATE=1 to reuse an existing web build.
set -euo pipefail

DEMO_NAME=m6
DEMO_PORT="${TDC_DEMO_PORT:-8306}"
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
api POST /api/directory/profiles/neema/enquiries "$CLIENT" \
  '{"subject":"Clinic booking app","body":"Online booking for three clinics."}' >/dev/null
expect_status 201 "baraka sends neema an enquiry"
api POST "/api/directory/enquiries/$(json 'd["id"]')/accept" "$NEEMA" >/dev/null
expect_status 201 "neema takes it up"
ENGAGEMENT="$(json 'd["id"]')"
E="/api/directory/engagements/$ENGAGEMENT"

say "Neema proposes, in whole minor units of one currency"
api POST "$E/proposals" "$NEEMA" '{"scope":"Online booking for three clinics","timeline":"Six weeks from agreement","currency":"TZS","lineItems":[{"description":"Booking screens","amountMinor":150000000},{"description":"Reports","amountMinor":50000000}]}' >/dev/null
expect_status 201 "neema sends version 1"
V1="$(json 'd["id"]')"
check 'd["version"] == 1 and d["totalMinor"] == 200000000 and d["currency"] == "TZS"' \
  "the total is computed by the server: $(json 'd["totalMinor"]') minor units of TZS"
api POST "$E/proposals" "$NEEMA" '{"scope":"x","timeline":"y","currency":"TZS","lineItems":[{"description":"Half a shilling","amountMinor":-5}]}' >/dev/null
expect_status 400 "a negative price is refused"

say "Baraka asks for a change to the second line, and neema is told"
api GET /api/me/notifications/count "$NEEMA" >/dev/null
BEFORE="$(json 'd["unread"]')"
api POST "$E/proposals/$V1/responses" "$CLIENT" '{"kind":"request_changes","part":"item:7","comment":"?"}' >/dev/null
expect_status 400 "a change request naming a line that is not there is refused"
api POST "$E/proposals/$V1/responses" "$CLIENT" '{"kind":"request_changes","part":"item:2","comment":"Monthly totals per clinic, please"}' >/dev/null
expect_status 201 "baraka asks for a change to line 2"
check 'd["state"] == "negotiation"' "the engagement moves to negotiation"
api GET /api/me/notifications/count "$NEEMA" >/dev/null
check "d['unread'] > $BEFORE" "neema has a new notification ($BEFORE -> $(json 'd["unread"]'))"
api GET "$E/proposals" "$NEEMA" >/dev/null
check 'd["responses"][-1]["part"] == "item:2" and d["responses"][-1]["comment"].startswith("Monthly")' \
  "the request is attached to line 2: $(json 'd["responses"][-1]')"

say "Neema sends a revised version; the first stays as it was"
api POST "$E/proposals" "$NEEMA" '{"scope":"Online booking for three clinics","timeline":"Six weeks from agreement","currency":"TZS","lineItems":[{"description":"Booking screens","amountMinor":150000000},{"description":"Reports with monthly totals per clinic","amountMinor":65000000}]}' >/dev/null
expect_status 201 "neema sends version 2"
V2="$(json 'd["id"]')"
api GET "$E/proposals" "$CLIENT" >/dev/null
check 'len(d["versions"]) == 2 and d["versions"][0]["lineItems"][1]["amountMinor"] == 50000000 and d["versions"][1]["totalMinor"] == 215000000' \
  "baraka sees both versions; version 1 still says 500,000.00 for reports"

say "One acceptance agrees nothing; both on the same version records it"
api POST "$E/proposals/$V1/responses" "$CLIENT" '{"kind":"accept"}' >/dev/null
expect_status 409 "the superseded version cannot be accepted"
api POST "$E/proposals/$V2/responses" "$CLIENT" '{"kind":"accept"}' >/dev/null
expect_status 201 "baraka accepts version 2"
check 'd["state"] == "proposal"' "with only baraka's acceptance it is still a proposal"
api GET "$E/proposals" "$NEEMA" >/dev/null
check '"agreement" not in d' "and no agreement is recorded"
api POST "$E/proposals/$V2/responses" "$NEEMA" '{"kind":"accept"}' >/dev/null
expect_status 201 "neema accepts version 2"
check 'd["state"] == "agreement"' "the engagement is now an agreement"
api GET "$E/proposals" "$CLIENT" >/dev/null
check 'd["agreement"]["version"] == 2 and d["agreement"]["clientAcceptedAt"] and d["agreement"]["developerAcceptedAt"]' \
  "recorded with both acceptance times: $(json '(d["agreement"]["clientAcceptedAt"], d["agreement"]["developerAcceptedAt"])')"
SHOWN="$(json 'd["disclosure"]')"

say "The record cannot be rewritten"
REFUSED="$(docker exec tdc-database-1 psql -U tdc -d "$DATABASE" -tAc \
  "UPDATE engagement_agreements SET terms = '{}'::jsonb" 2>&1 || true)"
[[ "$REFUSED" == *"cannot be changed"* ]] || fail "the database let the agreement be rewritten: $REFUSED"
pass "even the database owner is refused: ${REFUSED%%$'\n'*}"
REFUSED="$(docker exec tdc-database-1 psql -U tdc -d "$DATABASE" -tAc \
  "UPDATE proposal_versions SET total_minor = 1" 2>&1 || true)"
[[ "$REFUSED" == *"cannot be changed"* ]] || fail "a sent version was rewritten: $REFUSED"
pass "and so is changing a sent version"

say "Both export the same agreement, under the same statement"
api GET "$E/agreement/export" "$NEEMA" >"$WORK/neema.json"
expect_status 200 "neema exports it"
api GET "$E/agreement/export" "$CLIENT" >"$WORK/baraka.json"
expect_status 200 "baraka exports it"
python3 - "$WORK/neema.json" "$WORK/baraka.json" "$SHOWN" <<'EOF' || fail "the two exports differ"
import json, sys
a, b = (json.load(open(p)) for p in sys.argv[1:3])
for d in (a, b):
    d.pop("exportedAt")
assert a == b, (a, b)
assert a["format"] == "tdc.directory.agreement/1"
assert a["disclosure"] == sys.argv[3]
assert a["terms"]["totalMinor"] == 215000000 and a["terms"]["currency"] == "TZS"
EOF
pass "identical but for when each was exported, with the statement the screen shows:"
note "$SHOWN"
api GET "$E/agreement/export" "$OTHER" >/dev/null
expect_status 404 "somebody outside the engagement cannot export it"

say "Neema says the work is complete; it stays outstanding until baraka confirms"
api POST "$E/start" "$CLIENT" >/dev/null
expect_status 200 "baraka starts the work"
api POST "$E/complete" "$NEEMA" '{"note":"Deployed to all three clinics"}' >/dev/null
expect_status 200 "neema says it is complete"
check 'd["state"] == "completion_pending" and d["completionRequestedBy"] == "neema"' \
  "outstanding, asked by neema"
api POST "$E/confirm-completion" "$NEEMA" >/dev/null
expect_status 403 "neema cannot confirm her own completion"
api GET "$E" "$CLIENT" >/dev/null
check 'd["state"] == "completion_pending"' "baraka sees it still outstanding"
api POST "$E/confirm-completion" "$CLIENT" >/dev/null
expect_status 200 "baraka confirms"
for who in NEEMA CLIENT; do
  api GET "$E" "${!who}" >/dev/null
  check 'd["state"] == "completed"' "${who,,} sees it completed"
done
check '[t["to"] for t in d["transitions"]] == ["discussion","proposal","negotiation","proposal","agreement","active","completion_pending","completed"]' \
  "the history has every step: $(json '" -> ".join(t["to"] + " (" + t["actor"] + ")" for t in d["transitions"])')"

say "The whole cycle by two people on two phones"
browser tests/directory-agreement.spec.ts

say "Milestone demonstrated"
