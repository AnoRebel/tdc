#!/usr/bin/env bash
#
# The directory's first milestone: a profile exists, and only its owner
# controls it.
#
# Somebody with no profile uses chat as they always have. Somebody else creates
# a profile, leaves contact details at their default, and nothing about their
# privacy settings or presence moves. A moderator changes that profile and the
# audit log says who, what and why. Then the same things happen in a browser at
# phone width.
#
# Every claim is asserted; the script stops at the first one that does not hold.
#
# Usage: scripts/demo-milestone-directory-1.sh
#        TDC_DEMO_SKIP_GENERATE=1 to reuse an existing web build.
set -euo pipefail

DEMO_NAME=m1
DEMO_PORT="${TDC_DEMO_PORT:-8301}"
# shellcheck source=scripts/demo-directory-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-directory-common.sh"

build
start
founder_with_directory

JUMA="$(register juma)"
AMINA="$(register amina)"
KHERI="$(register kheri)"

say "Somebody with no profile uses chat normally"
api POST /api/rooms "$FOUNDER" '{"slug":"general","name":"General","visibility":"public"}' >/dev/null
expect_status 201 "a channel exists"
ROOM="$(json 'd["id"]')"
for token in "$JUMA" "$AMINA"; do
  api POST "/api/rooms/$ROOM/join" "$token" >/dev/null
done
api GET /api/directory/me "$JUMA" >/dev/null
expect_status 404 "juma has no directory profile"
api POST "/api/rooms/$ROOM/messages" "$JUMA" '{"body":"Habari za asubuhi","clientToken":"m1-1"}' >/dev/null
expect_status 201 "juma posts in the channel all the same"
api GET "/api/rooms/$ROOM/timeline" "$AMINA" >/dev/null
check 'any("Habari za asubuhi" in json.dumps(e) for e in d)' "amina reads what juma wrote"

say "Amina creates a profile, leaving contact details at their default"
api PUT /api/me/privacy "$AMINA" '{"directMessages":"shared","showsLastSeen":false}' >/dev/null
expect_status 204 "amina has already chosen some privacy settings"
PRIVACY_BEFORE="$(api GET /api/me/privacy "$AMINA")"
api GET "/api/rooms/$ROOM/members" "$JUMA" >/dev/null
MEMBER_BEFORE="$(json 'sorted(next(m for m in d if m["username"]=="amina").keys())')"

api PUT /api/directory/me "$AMINA" '{
  "headline": "Backend developer",
  "bio": "Go, Postgres, and payments integrations.",
  "location": "Dar es Salaam",
  "contactEmail": "amina@example.com",
  "contactPhone": "+255 700 000 001",
  "visibility": "members",
  "portfolio": [{"title": "Clinic booking", "url": "https://example.com/clinic"}]
}' >/dev/null
expect_status 201 "the profile is created"
check 'd["profile"]["contactVisibility"] == "hidden"' "contact details default to hidden"

api GET /api/directory/profiles/amina "$JUMA" >/dev/null
expect_status 200 "juma, a member, can see the profile"
check '"contactEmail" not in d and "contactPhone" not in d' "juma is shown no contact details"
check 'd.get("headline") == "Backend developer"' "juma is shown what amina chose to show members"
api GET /api/directory/profiles/amina "" >/dev/null
expect_status 404 "a stranger cannot see a members-only profile at all"

PRIVACY_AFTER="$(api GET /api/me/privacy "$AMINA")"
[[ "$PRIVACY_BEFORE" == "$PRIVACY_AFTER" ]] \
  || fail "creating a profile changed privacy settings: $PRIVACY_BEFORE -> $PRIVACY_AFTER"
pass "amina's privacy settings are exactly as they were: $PRIVACY_AFTER"
api GET "/api/rooms/$ROOM/members" "$JUMA" >/dev/null
MEMBER_AFTER="$(json 'sorted(next(m for m in d if m["username"]=="amina").keys())')"
[[ "$MEMBER_BEFORE" == "$MEMBER_AFTER" ]] \
  || fail "the member list discloses something different: $MEMBER_BEFORE -> $MEMBER_AFTER"
pass "the member list discloses the same fields about her as before: $MEMBER_AFTER"
check '"lastSeenAt" not in next(m for m in d if m["username"]=="amina")' \
  "last seen stays withheld, as she chose before the directory existed"

say "Only a moderator can change somebody else's profile, and it is recorded"
MODERATION='{"headline":"Backend developer","bio":"","location":"Dar es Salaam","visibility":"hidden","reason":"bio carried a spam link"}'
api PUT /api/directory/moderation/profiles/amina "$JUMA" "$MODERATION" >/dev/null
expect_status 403 "juma, an ordinary member, is refused"
grant_role "$KHERI" directory.profile.manage
api PUT /api/directory/moderation/profiles/amina "$KHERI" "$MODERATION" >/dev/null
expect_status 200 "kheri, holding directory.profile.manage, clears the bio and hides the profile"
api GET /api/directory/profiles/amina "$JUMA" >/dev/null
expect_status 404 "the hidden profile has left the directory"

api GET /api/me "$AMINA" >/dev/null
AMINA_ID="$(json 'd["id"]')"
api GET "/api/admin/audit?action=directory.profile.moderated" "$FOUNDER" >/dev/null
check "len(d) == 1 and d[0]['actor'] == 'kheri' and d[0]['targetId'] == '$AMINA_ID'" \
  "the audit log names the moderator and whose profile it was"
check '"bio carried a spam link" in d[0]["detail"] and "bio" in d[0]["detail"]' \
  "and says which fields changed, and why"
check '"payments integrations" not in d[0]["detail"]' "without copying what was removed"
note "$(json 'd[0]["occurredAt"] + "  " + d[0]["action"] + "  " + d[0]["detail"]')"

say "The same, in a browser at phone width"
browser tests/directory-profile.spec.ts

say "Milestone demonstrated"
