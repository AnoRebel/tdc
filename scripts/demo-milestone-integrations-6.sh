#!/usr/bin/env bash
#
# Bots, demonstrated.
#
# An approved application posts as a bot: its message carries the server's mark
# and a person's does not, and in a real browser the page names it and marks it
# as a bot in words. A moderator suspends it and its next post is refused. Then
# a second application — holding no live connection at all, only an invocation
# address — is sent a signed invocation when a person presses its button: the
# guide's worked-example variant, run as written, verifies it and answers.
#
# Usage: scripts/demo-milestone-integrations-6.sh
set -euo pipefail

PORT="${TDC_DEMO_PORT:-8209}"
RECEIVER_PORT="${TDC_DEMO_RECEIVER_PORT:-8204}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-integrations-common.sh"
GUIDE="$ROOT/docs/webhooks-and-apps.md"
BASE="http://localhost:${PORT}"

build_server
build_web
WEB_ROOT="$ROOT/apps/web/.output/public" start_server m6 "$PORT"
FOUNDER="$(setup_founder "$PORT")"
setting "$PORT" "$FOUNDER" integrations.allow_private_addresses true

CHANNEL="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/rooms \
  '{"slug":"deploys","name":"deploys","visibility":"public"}')" | json '["id"]')"

# install NAME — registers and approves an application asking for
# channel.post in #deploys, printing its identifier and credential.
install() {
  local registered app approved
  registered="$(api "$PORT" "$FOUNDER" POST /api/admin/applications \
    "{\"name\":\"$1\",\"purpose\":\"Posts deployment results.\",\"permissions\":[\"channel.post\"]}")"
  [[ "$(status_of "$registered")" == "201" ]] || fail "registering $1: $(body_of "$registered")"
  app="$(body_of "$registered" | json '["id"]')"
  approved="$(api "$PORT" "$FOUNDER" POST "/api/admin/applications/${app}/approve" \
    "{\"permissions\":[\"channel.post\"],\"channels\":[\"${CHANNEL}\"]}")"
  [[ "$(status_of "$approved")" == "200" ]] || fail "approving $1: $(body_of "$approved")"
  echo "$app $(body_of "$approved" | json '["credential"]["token"]')"
}

say "An approved application posts as a bot"
read -r APP APP_TOKEN <<<"$(install "Deploy bot")"
expect_status "$(api "$PORT" "$APP_TOKEN" POST "/api/rooms/${CHANNEL}/messages" '{"body":"Deployed build 41."}')" \
  201 "the bot posting"
expect_status "$(api "$PORT" "$FOUNDER" POST "/api/rooms/${CHANNEL}/messages" '{"body":"Thanks."}')" \
  201 "a person posting"
body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${CHANNEL}/timeline")" | python3 -c '
import json, sys
by_body = {(e.get("content") or {}).get("body"): e for e in json.load(sys.stdin)}
bot, person = by_body["Deployed build 41."], by_body["Thanks."]
via = bot["content"].get("via") or {}
if via.get("kind") != "bot" or via.get("name") != "Deploy bot":
    raise SystemExit("the bot message is marked %r" % via)
if "via" in person["content"]:
    raise SystemExit("the person message is marked %r" % person["content"]["via"])
print("  %s: marked as a bot, named %r" % (bot["sender"], via["name"]))
print("  %s: no mark" % person["sender"])
' || fail "the bot is not distinguishable from a person"

say "In the browser, the page names it and marks it as a bot"
(cd "$ROOT/apps/web" && TDC_TEST_URL="$BASE" \
  bunx playwright test --project=setup --project=chromium tests/applications.spec.ts --reporter=list) |
  tee "$WORK/browser.out" || fail "the browser half failed; see the output above"
grep -qE "✓.*applications\.spec\.ts" "$WORK/browser.out" || fail "the browser half did not run"
note "the bot's message carries one mark, reading Bot, and the person's beside it none"
note "the desktop client renders messages with the same component, and so the same mark"

say "A moderator suspends it, and its next post is refused"
ACCOUNT="$(body_of "$(api "$PORT" "$FOUNDER" GET "/api/admin/applications/${APP}")" | json '["accountId"]')"
expect_status "$(api "$PORT" "$FOUNDER" POST "/api/admin/users/${ACCOUNT}/suspend" \
  '{"reason":"Posting in a loop"}')" 204 "suspending the bot's account"
expect_status "$(api "$PORT" "$APP_TOKEN" POST "/api/rooms/${CHANNEL}/messages" '{"body":"still here"}')" \
  401 "the suspended bot posting"
body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${CHANNEL}/timeline")" | grep -q "still here" &&
  fail "the refused post reached the channel"
note "nothing reached the channel"

say "A second application, with only an invocation address"
read -r RELEASE RELEASE_TOKEN <<<"$(install "Release bot")"
OUT="$(APP="$RELEASE" TOKEN="$FOUNDER" INVOCATION_URL="http://localhost:${RECEIVER_PORT}/presses" \
  run_doc_example set-invocation "$PORT" "$GUIDE")"
expect_doc_statuses "$OUT" "setting its invocation address"
SECRET="$(grep -E '^\{' <<<"$OUT" | head -1 | json '["secret"]')"
[[ "$SECRET" == whsec_* ]] || fail "no secret was shown: ${OUT}"
note "the secret is shown once"

say "The guide's worked-example variant, run as written"
doc_example verify-delivery "$PORT" >"$WORK/verify-delivery.ts"
doc_example invoked-bot "$PORT" >"$WORK/deploybot.ts"
[[ -s "$WORK/verify-delivery.ts" && -s "$WORK/deploybot.ts" ]] || fail "the guide's examples are missing"
grep -q "WebSocket\|/api/live" "$WORK/deploybot.ts" && fail "the variant holds a live connection"
note "the program opens no live connection: it only listens for invocations"
PORT="$RECEIVER_PORT" TDC_SERVER="$BASE" TDC_TOKEN="$RELEASE_TOKEN" TDC_CHANNEL="$CHANNEL" \
  TDC_INVOCATION_SECRET="$SECRET" bun run --cwd "$WORK" deploybot.ts >"$WORK/deploybot.log" 2>&1 &
BOT_PID=$!
HELPER_PIDS="${HELPER_PIDS:-} ${BOT_PID}"

QUESTION=""
for _ in $(seq 1 40); do
  QUESTION="$(body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${CHANNEL}/timeline")" | python3 -c '
import json, sys
for e in json.load(sys.stdin):
    c = e.get("content") or {}
    if c.get("body") == "Deploy build 41 to production?" and (c.get("via") or {}).get("name") == "Release bot":
        print(e["id"])
')" || true
  [[ -n "$QUESTION" ]] && break
  sleep 0.5
done
[[ -n "$QUESTION" ]] || { cat "$WORK/deploybot.log"; fail "the program did not ask its question"; }
note "it asked, as Release bot"

say "A person presses Deploy"
JUMA="$(register "$PORT" juma)"
expect_status "$(api "$PORT" "$JUMA" POST "/api/rooms/${CHANNEL}/join")" 200 "juma joining"
ENCODED="$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$QUESTION")"
expect_status "$(api "$PORT" "$JUMA" POST "/api/rooms/${CHANNEL}/messages/${ENCODED}/actions/deploy")" \
  201 "pressing Deploy"

ANSWERED=""
for _ in $(seq 1 40); do
  if body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${CHANNEL}/timeline")" |
    grep -q "Deploying build 41, at juma's word."; then
    ANSWERED=1
    break
  fi
  sleep 0.5
done
[[ -n "$ANSWERED" ]] || { cat "$WORK/deploybot.log"; fail "the program was not told, or did not answer"; }
note "the program verified the invocation and answered: Deploying build 41, at juma's word."

body_of "$(api "$PORT" "$FOUNDER" GET "/api/admin/applications/${RELEASE}/invocations")" | python3 -c '
import json, sys
invocations = json.load(sys.stdin)
if not invocations or invocations[0]["state"] != "delivered":
    raise SystemExit("the history shows %r" % invocations)
print("  its history: %s after %d attempt(s), HTTP %s" % (invocations[0]["state"], invocations[0]["attempts"], invocations[0].get("lastStatus")))
' || fail "the invocation is not recorded as delivered"

for _ in $(seq 1 20); do kill -0 "$BOT_PID" 2>/dev/null || break; sleep 0.25; done
kill -0 "$BOT_PID" 2>/dev/null && fail "the program did not finish after answering"

say "Milestone demonstrated"
note "the bot's messages are marked as a bot's, by the server and on the page"
note "suspending it refused its next post"
note "an application with no connection was sent a signed invocation, verified it with the"
note "guide's own code, and answered — the documented variant, run as written"
