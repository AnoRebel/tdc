#!/usr/bin/env bash
#
# Applications, demonstrated.
#
# The guide's request registers an application asking for channel.post; what
# an approver is shown before approving is exactly that, with what it allows;
# approving anything else is refused; the guide's approval scopes it to one
# channel and hands over a credential once; that credential posts in that
# channel, as the application, and nowhere else; an ordinary member — no
# administrator — finds it listed with its permissions, and as able to act in
# the channel; and the guide's removal stops it at once and keeps what it posted.
#
# Usage: scripts/demo-milestone-integrations-5.sh
set -euo pipefail

PORT="${TDC_DEMO_PORT:-8209}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-integrations-common.sh"
GUIDE="$ROOT/docs/webhooks-and-apps.md"

build_server
start_server m5 "$PORT"
FOUNDER="$(setup_founder "$PORT")"

CHANNEL="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/rooms \
  '{"slug":"deploys","name":"deploys","visibility":"public"}')" | json '["id"]')"
OTHER="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/rooms \
  '{"slug":"random","name":"random","visibility":"public"}')" | json '["id"]')"

say "The guide's request registers an application asking for channel.post"
OUT="$(TOKEN="$FOUNDER" run_doc_example register-application "$PORT" "$GUIDE")"
expect_doc_statuses "$OUT" "registering"
APP="$(grep -E '^\{' <<<"$OUT" | head -1 | json '["id"]')"

say "Before approving, the approver is shown exactly what it will hold"
SHOWN="$(body_of "$(api "$PORT" "$FOUNDER" GET "/api/admin/applications/${APP}")")"
CATALOGUE="$(body_of "$(api "$PORT" "$FOUNDER" GET /api/admin/applications/permissions)")"
python3 -c '
import json, sys
shown, catalogue = json.loads(sys.argv[1]), json.loads(sys.argv[2])
if shown["state"] != "registered" or shown["granted"]:
    raise SystemExit("registering granted something: %r" % shown["granted"])
if shown["requested"] != ["channel.post"]:
    raise SystemExit("it is shown asking for %r" % shown["requested"])
described = {p["name"]: p for p in catalogue}
for name in shown["requested"]:
    p = described[name]
    print("  %s — %s (per channel: %s, administrative: %s)" % (name, p["description"], p["perChannel"], p["administrative"]))
print("  sends off the server: %s" % shown["externalDisclosure"])
' "$SHOWN" "$CATALOGUE" || fail "the approver is not shown what it asks for"

CHANGED="$(api "$PORT" "$FOUNDER" POST "/api/admin/applications/${APP}/approve" \
  "{\"permissions\":[\"channel.post\",\"channel.read\"],\"channels\":[\"${CHANNEL}\"]}")"
expect_status "$CHANGED" 409 "approving a list other than the one shown"

say "The guide's approval scopes it to #deploys"
OUT="$(APP="$APP" CHANNEL="$CHANNEL" TOKEN="$FOUNDER" run_doc_example approve-application "$PORT" "$GUIDE")"
expect_doc_statuses "$OUT" "approving"
APPROVED="$(grep -E '^\{' <<<"$OUT" | head -1)"
APP_TOKEN="$(json '["credential"]["token"]' <<<"$APPROVED")"
ACCOUNT="$(json '["account"]' <<<"$APPROVED")"
python3 -c '
import json, sys
a = json.loads(sys.argv[1])
if a["granted"] != ["channel.post"] or [c["name"] for c in a["channels"]] != ["deploys"]:
    raise SystemExit("approved as %r in %r" % (a["granted"], a["channels"]))
print("  granted %s in #%s, as %s" % (a["granted"], a["channels"][0]["name"], a["account"]))
' "$APPROVED" || fail "it was not approved as shown"
AGAIN="$(body_of "$(api "$PORT" "$FOUNDER" GET "/api/admin/applications/${APP}")")"
grep -q "$APP_TOKEN" <<<"$AGAIN" && fail "the credential is shown again"
note "the credential is not shown again"

say "The guide's curl posts in #deploys, as the application"
OUT="$(CHANNEL="$CHANNEL" APP_TOKEN="$APP_TOKEN" run_doc_example post-as-application "$PORT" "$GUIDE")"
expect_doc_statuses "$OUT" "posting with the credential"
body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${CHANNEL}/timeline")" | python3 -c '
import json, sys
for event in json.load(sys.stdin):
    if "Deployed build 41" in (event.get("content") or {}).get("body", ""):
        if event["sender"] != sys.argv[1]:
            raise SystemExit("posted as %s" % event["sender"])
        print("  sent by %s" % event["sender"])
        break
else:
    raise SystemExit("the message is not in the channel")
' "$ACCOUNT" || fail "the post is not the application's"

say "And nowhere else"
expect_status "$(api "$PORT" "$APP_TOKEN" POST "/api/rooms/${OTHER}/messages" '{"body":"not here"}')" \
  403 "posting in #random"
expect_status "$(api "$PORT" "$APP_TOKEN" POST "/api/rooms/${OTHER}/join")" 403 "joining #random"
expect_status "$(api "$PORT" "$APP_TOKEN" GET "/api/rooms/${CHANNEL}/timeline")" \
  403 "reading #deploys, which channel.post does not allow"
expect_status "$(api "$PORT" "$APP_TOKEN" GET /api/admin/applications)" 403 "managing applications"
expect_status "$(api "$PORT" "$APP_TOKEN" POST /api/rooms '{"slug":"mine","name":"mine"}')" \
  403 "making a channel"
body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${OTHER}/timeline")" | grep -q "not here" &&
  fail "the refused post reached #random"
note "nothing reached #random"

say "An ordinary member finds it listed, with its permissions"
MEMBER="$(register "$PORT" member)"
expect_status "$(api "$PORT" "$MEMBER" GET /api/admin/applications)" 403 "the member managing applications"
expect_status "$(api "$PORT" "$MEMBER" POST "/api/rooms/${CHANNEL}/join")" 200 "the member joining #deploys"
INSTALLED="$(body_of "$(api "$PORT" "$MEMBER" GET /api/applications)")"
IN_ROOM="$(body_of "$(api "$PORT" "$MEMBER" GET "/api/rooms/${CHANNEL}/applications")")"
python3 -c '
import json, sys
installed, in_room = json.loads(sys.argv[1]), json.loads(sys.argv[2])
match = [a for a in installed if a["name"] == "Deploy bot"]
if not match or match[0]["permissions"] != ["channel.post"]:
    raise SystemExit("not listed with its permissions: %r" % installed)
a = match[0]
print("  %s: %s, in %d channel(s); sends off the server: %s" % (a["name"], a["permissions"], a["channelCount"], a["externalDisclosure"]))
if [x["name"] for x in in_room] != ["Deploy bot"]:
    raise SystemExit("#deploys does not list it: %r" % in_room)
print("  #deploys lists it as able to act there")
' "$INSTALLED" "$IN_ROOM" || fail "an ordinary member cannot see it"

say "The guide's removal stops it at once, and keeps what it posted"
OUT="$(APP="$APP" TOKEN="$FOUNDER" run_doc_example remove-application "$PORT" "$GUIDE")"
expect_doc_statuses "$OUT" "removing"
grep -q '"messagesRemoved":0' <<<"$OUT" || fail "removal took messages down: ${OUT}"
expect_status "$(api "$PORT" "$APP_TOKEN" POST "/api/rooms/${CHANNEL}/messages" '{"body":"after"}')" \
  401 "the credential's next request"
body_of "$(api "$PORT" "$MEMBER" GET "/api/rooms/${CHANNEL}/timeline")" | grep -q "Deployed build 41" ||
  fail "what it posted is gone"
note "its message is still in #deploys"
body_of "$(api "$PORT" "$MEMBER" GET /api/applications)" | grep -q "Deploy bot" &&
  fail "it is still listed as installed"
note "and it is no longer listed as installed"

say "Milestone demonstrated"
note "registered asking for channel.post, approved having been shown exactly that, in one channel"
note "its credential posted there as the application, and was refused everywhere else"
note "an ordinary member saw it listed with its permissions; removal kept what it posted"
