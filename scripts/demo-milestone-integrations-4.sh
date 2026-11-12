#!/usr/bin/env bash
#
# Incoming webhooks, demonstrated.
#
# An incoming webhook is made on a channel with the guide's request; the guide's
# curl posts through it and the message is the webhook's, not a person's; the
# same address naming a different channel is refused; revoking it refuses the
# very next call; and the audit log holds both the creation and the revocation,
# with who did them.
#
# Usage: scripts/demo-milestone-integrations-4.sh
set -euo pipefail

PORT="${TDC_DEMO_PORT:-8208}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-integrations-common.sh"
GUIDE="$ROOT/docs/webhooks-and-apps.md"

build_server
start_server m4 "$PORT"
FOUNDER="$(setup_founder "$PORT")"

ROOM="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/rooms \
  '{"slug":"builds","name":"builds","visibility":"public"}')" | json '["id"]')"
OTHER="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/rooms \
  '{"slug":"random","name":"random","visibility":"public"}')" | json '["id"]')"

say "The guide's request makes an incoming webhook on #builds"
OUT="$(ROOM="$ROOM" TOKEN="$FOUNDER" run_doc_example create-incoming "$PORT" "$GUIDE")"
expect_doc_statuses "$OUT" "creating the webhook"
CREATED="$(grep -E '^\{' <<<"$OUT" | head -1)"
HOOK="$(json '["id"]' <<<"$CREATED")"
WEBHOOK_URL="$(json '["url"]' <<<"$CREATED")"
[[ "$WEBHOOK_URL" == "http://localhost:${PORT}/api/hooks/tdc_hook_"* ]] ||
  fail "the address is not what the guide describes: ${WEBHOOK_URL}"
note "address: ${WEBHOOK_URL%%tdc_hook_*}tdc_hook_… (shown once)"

LISTED="$(body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${ROOM}/incoming-webhooks")")"
grep -q "${WEBHOOK_URL##*/}" <<<"$LISTED" && fail "the listing shows the credential again"
note "the listing does not show the address again"

say "The guide's curl posts through it"
OUT="$(WEBHOOK_URL="$WEBHOOK_URL" run_doc_example post-incoming "$PORT" "$GUIDE")"
expect_doc_statuses "$OUT" "posting through the webhook"

body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${ROOM}/timeline")" | python3 -c '
import json, sys
for event in json.load(sys.stdin):
    content = event.get("content") or {}
    if "Build 41 failed" in content.get("body", ""):
        via = content.get("via") or {}
        print("  sender %s, shown as %r, marked %s" % (event["sender"], via.get("name"), via.get("kind")))
        if via.get("kind") != "webhook" or via.get("name") != "Build server":
            raise SystemExit("the message is not attributed to the webhook")
        if event["sender"].startswith("founder"):
            raise SystemExit("the message reads as the person who made the webhook")
        if not content.get("actions") or content["actions"][0].get("capability") != "open":
            raise SystemExit("the link button did not arrive")
        break
else:
    raise SystemExit("the message is not in the channel")
' || fail "the post is not the webhook's"

say "The same address, naming #random, is refused"
REFUSED="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$WEBHOOK_URL" -H 'Content-Type: application/json' \
  -d "{\"text\":\"posted elsewhere\",\"room\":\"${OTHER}\"}")"
[[ "$REFUSED" == "403" ]] || fail "naming another channel answered ${REFUSED}"
note "naming another channel: 403"
body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${OTHER}/timeline")" | grep -q "posted elsewhere" &&
  fail "the refused post reached #random"
body_of "$(api "$PORT" "$FOUNDER" GET "/api/rooms/${ROOM}/timeline")" | grep -q "posted elsewhere" &&
  fail "the refused post reached #builds"
note "and nothing was posted in either channel"

say "Revoked, and the very next call is refused"
REVOKED="$(api "$PORT" "$FOUNDER" DELETE "/api/rooms/${ROOM}/incoming-webhooks/${HOOK}")"
expect_status "$REVOKED" 204 "revoking"
NEXT="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$WEBHOOK_URL" -H 'Content-Type: application/json' \
  -d '{"text":"after revocation"}')"
[[ "$NEXT" == "404" ]] || fail "the next call after revocation answered ${NEXT}"
note "the next call: 404, with nothing in between"

say "Both are in the audit log, with who did them"
body_of "$(api "$PORT" "$FOUNDER" GET /api/admin/audit)" | python3 -c '
import json, sys
hook = sys.argv[1]
found = {entry["action"]: entry.get("actor") for entry in json.load(sys.stdin) if entry.get("targetId") == hook}
for action in ("integration.incoming_webhook.created", "integration.incoming_webhook.revoked"):
    if found.get(action) != "founder":
        raise SystemExit("%s recorded with actor %r" % (action, found.get(action)))
    print("  %-40s by %s" % (action, found[action]))
' "$HOOK" || fail "the audit log does not hold both"

say "Milestone demonstrated"
note "a curl through the guide's request posted a message marked as the webhook's"
note "naming another channel was refused; revocation refused the very next call"
note "creation and revocation are in the audit log with their actor"
