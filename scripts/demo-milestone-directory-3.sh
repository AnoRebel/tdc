#!/usr/bin/env bash
#
# The directory's third milestone: verification.
#
# A moderator verifies one skill on a profile that has three, and the profile
# shows one verified claim and two self-asserted ones. The owner edits the
# verified skill and it reverts to self-asserted. A moderator revokes a separate
# verification. The audit log says who, when and why for every step. Then the
# same from the interface, at phone width, with the statement of what a
# verification means read on three screens.
#
# Usage: scripts/demo-milestone-directory-3.sh
#        TDC_DEMO_SKIP_GENERATE=1 to reuse an existing web build.
set -euo pipefail

DEMO_NAME=m3
DEMO_PORT="${TDC_DEMO_PORT:-8303}"
# shellcheck source=scripts/demo-directory-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-directory-common.sh"

build
start
founder_with_directory

AMINA="$(register amina)"
KHERI="$(register kheri)"
CLIENT="$(register baraka)"
grant_role "$KHERI" directory.verify

PROFILE='{
  "visibility": "members",
  "skills": [
    {"name": "Go", "description": "eight years, payment integrations"},
    {"name": "PostgreSQL", "description": "six years"},
    {"name": "Docker", "description": "every day"}
  ],
  "portfolio": [{"title": "Clinic booking", "url": "https://example.com/clinic"}]
}'
api PUT /api/directory/me "$AMINA" "$PROFILE" >/dev/null
expect_status 201 "amina lists three skills and a portfolio link, all self-asserted"

say "Kheri verifies one of the three skills"
api GET /api/directory/moderation/profiles/amina/claims "$CLIENT" >/dev/null
expect_status 403 "a member without directory.verify cannot even read the claims"
api GET /api/directory/moderation/profiles/amina/claims "$KHERI" >/dev/null
GO="$(json 'next(c["id"] for c in d["claims"] if c["label"] == "Go")')"
CLINIC="$(json 'next(c["id"] for c in d["claims"] if c["label"] == "Clinic booking")')"

api POST /api/directory/moderation/profiles/amina/verifications "$KHERI" \
  "{\"kind\":\"skill\",\"claimId\":\"$GO\",\"note\":\"\"}" >/dev/null
expect_status 400 "a verification without a note on what was checked is refused"
api POST /api/directory/moderation/profiles/amina/verifications "$KHERI" \
  "{\"note\":\"looked at her whole profile\"}" >/dev/null
expect_status 400 "a verification that does not name one claim is refused"
api POST /api/directory/moderation/profiles/amina/verifications "$KHERI" \
  "{\"kind\":\"skill\",\"claimId\":\"$GO\",\"note\":\"Reviewed three of her public Go repositories\"}" >/dev/null
expect_status 201 "kheri verifies Go, saying what he checked"
api POST /api/directory/moderation/profiles/amina/verifications "$KHERI" \
  "{\"kind\":\"portfolio\",\"claimId\":\"$CLINIC\",\"note\":\"Visited the live site\"}" >/dev/null
expect_status 201 "and, separately, the portfolio link"
LINK_VERIFICATION="$(json 'd["id"]')"

api GET /api/directory/profiles/amina "$CLIENT" >/dev/null
check '[s["name"] for s in d["skills"] if s.get("verified")] == ["Go"]' "a client sees Go verified"
check '[s["name"] for s in d["skills"] if not s.get("verified")] == ["PostgreSQL", "Docker"]' \
  "and PostgreSQL and Docker self-asserted, distinguishably"
check '"identityVerified" not in d' "with nothing implying the person as a whole was checked"
check '"Reviewed three" not in json.dumps(d)' "and without the moderator's note, which is for amina"

say "Amina edits the verified skill; it reverts to self-asserted"
# The link is sent back with its identifier: the same link, unedited. Sent
# without it, it would be a new link, and the old one's verification would go
# with the old one.
PROFILE="${PROFILE/\"title\": \"Clinic booking\"/\"id\": \"$CLINIC\", \"title\": \"Clinic booking\"}"
api PUT /api/directory/me "$AMINA" "${PROFILE/eight years, payment integrations/twelve years}" >/dev/null
expect_status 200 "amina changes what she says about Go"
api GET /api/directory/profiles/amina "$CLIENT" >/dev/null
check 'not any(s.get("verified") for s in d["skills"])' "Go is no longer shown as verified"
check 'd["portfolio"][0].get("verified")' "the link she did not edit is still verified"
api PUT /api/directory/me "$AMINA" "$PROFILE" >/dev/null
api GET /api/directory/profiles/amina "$CLIENT" >/dev/null
check 'not any(s.get("verified") for s in d["skills"])' \
  "and editing it back does not restore it: nobody checked what is there now"

say "Kheri revokes the other verification"
api POST "/api/directory/moderation/verifications/$LINK_VERIFICATION/revoke" "$KHERI" '{"reason":""}' >/dev/null
expect_status 400 "a revocation without a reason is refused"
api POST "/api/directory/moderation/verifications/$LINK_VERIFICATION/revoke" "$KHERI" \
  '{"reason":"The site turned out to be another developer'"'"'s work"}' >/dev/null
expect_status 204 "kheri revokes the link's verification, saying why"
api GET /api/directory/profiles/amina "$CLIENT" >/dev/null
check 'not d["portfolio"][0].get("verified")' "it is gone at once"

say "The audit log: who, when and why, for every step"
api GET "/api/admin/audit?action=directory.verification.granted" "$FOUNDER" >/dev/null
check 'len(d) == 2 and all(e["actor"] == "kheri" for e in d)' "two grants, both by kheri"
check 'any("Reviewed three of her public Go repositories" in e["detail"] for e in d)' "each with what was checked"
for e in granted cleared revoked; do
  api GET "/api/admin/audit?action=directory.verification.$e" "$FOUNDER" >/dev/null
  note "$(json '"\n  ".join(e["occurredAt"] + "  " + e["actor"] + "  " + e["action"] + "  " + e["detail"] for e in d)')"
done
api GET "/api/admin/audit?action=directory.verification.cleared" "$FOUNDER" >/dev/null
check 'len(d) == 1 and d[0]["actor"] == "amina" and "edited" in d[0]["detail"]' \
  "the clearing is recorded against amina's edit, with why"
api GET "/api/admin/audit?action=directory.verification.revoked" "$FOUNDER" >/dev/null
check "len(d) == 1 and d[0]['actor'] == 'kheri' and d[0]['targetId'] == '$LINK_VERIFICATION' and \"another developer\" in d[0]['detail']" \
  "the revocation is recorded with kheri, the verification, and the reason"

say "The same from the interface, at phone width"
browser tests/directory-verify.spec.ts

say "Milestone demonstrated"
