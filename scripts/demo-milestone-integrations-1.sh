#!/usr/bin/env bash
#
# Interactive messages beyond buttons, demonstrated.
#
# A message offers a selection menu and an input field in the web client; a
# response replaces its controls for every viewer; a private response is
# visible only to the person who triggered it; and a stale second activation is
# refused. The browser half is a real Chromium driving the built web client;
# the documented examples are then run, as written, against the same server.
#
# Every claim is asserted. The script exits non-zero on the first that fails.
#
# Usage: scripts/demo-milestone-integrations-1.sh
set -euo pipefail

PORT="${TDC_DEMO_PORT:-8201}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-integrations-common.sh"
BASE="http://localhost:${PORT}"

build_server
build_web
WEB_ROOT="$ROOT/apps/web/.output/public" start_server m1 "$PORT"
FOUNDER="$(setup_founder "$PORT")"

say "In the browser: a menu and a field, answered, answered privately, and replaced"
(cd "$ROOT/apps/web" && TDC_TEST_URL="$BASE" TDC_DEMO_INTEGRATIONS=1 \
  bunx playwright test --project=chromium tests/integrations-milestone-1.spec.ts --reporter=list) |
  tee "$WORK/browser.out" || fail "the browser half failed; see the output above"
grep -qE "✓.*integrations-milestone-1\.spec\.ts" "$WORK/browser.out" ||
  fail "the browser half did not run, or was skipped"
note "a menu announced as a list of choices, and a field named by its label"
note "both answers reached the program carrying what was chosen and typed"
note "the private answer reached the presser's page and not the other person's"
note "the update replaced the controls on both pages, and a stale press was refused"

say "The documented examples, run as written"
ROOM="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/rooms \
  '{"slug":"documented","name":"documented","visibility":"public"}')" | json '["id"]')"
PROGRAM="$(register "$PORT" docbot)"
PERSON_TOKEN="$(register "$PORT" juma)"
api "$PORT" "$PROGRAM" POST "/api/rooms/${ROOM}/join" >/dev/null
api "$PORT" "$PERSON_TOKEN" POST "/api/rooms/${ROOM}/join" >/dev/null

OUT="$(ROOM="$ROOM" TOKEN="$PROGRAM" run_doc_example post-menu "$PORT")"
expect_doc_statuses "$OUT" "posting a menu and a field"
EVENT="$(grep -E '^\{' <<<"$OUT" | head -1 | json '["id"]')"

PRESSED="$(api "$PORT" "$PERSON_TOKEN" POST \
  "/api/rooms/${ROOM}/messages/$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$EVENT")/actions/environment" \
  '{"value":"staging","version":0}')"
expect_status "$PRESSED" 201 "juma chooses staging"
PRESS="$(body_of "$PRESSED" | json '["id"]')"

OUT="$(ROOM="$ROOM" TOKEN="$PROGRAM" PRESS="$PRESS" run_doc_example private-reply "$PORT")"
expect_doc_statuses "$OUT" "answering juma privately"
grep -q '"delivered":0' <<<"$OUT" ||
  fail "the documented reply claims a delivery to somebody with no open connection"
note "delivered to nobody, honestly, since juma has no live connection open"

PERSON="$(api "$PORT" "$FOUNDER" GET "/api/search/everything?q=juma&messages=false" | sed '$d' |
  python3 -c 'import json,sys; print([p["id"] for p in json.load(sys.stdin)["people"] if p["username"]=="juma"][0])')"
OUT="$(ROOM="$ROOM" TOKEN="$PROGRAM" PERSON="$PERSON" run_doc_example context "$PORT")"
expect_doc_statuses "$OUT" "remembering the exchange and reading it back"
grep -q 'chose-environment' <<<"$OUT" || fail "the documented read did not return what was stored"

SNOOP="$(api "$PORT" "$PERSON_TOKEN" GET "/api/rooms/${ROOM}/context/${PERSON}")"
expect_status "$SNOOP" 404 "juma reading what the program remembers about him"

OUT="$(ROOM="$ROOM" TOKEN="$PROGRAM" EVENT="$EVENT" run_doc_example update-controls "$PORT")"
expect_doc_statuses "$OUT" "replacing the message's controls"

STALE="$(api "$PORT" "$PERSON_TOKEN" POST \
  "/api/rooms/${ROOM}/messages/$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$EVENT")/actions/environment" \
  '{"value":"production","version":0}')"
expect_status "$STALE" 409 "a second activation through the replaced view"

AGAIN="$(ROOM="$ROOM" TOKEN="$PROGRAM" EVENT="$EVENT" run_doc_example update-controls "$PORT" || true)"
grep -q '^HTTP 409$' <<<"$AGAIN" ||
  fail "repeating the documented update against version 0 was not refused as stale"
note "repeating the same update against the old version: 409"

say "Milestone demonstrated"
note "a menu and a field in the web client, answered with what was chosen and typed"
note "an update in place, seen by every viewer; a stale activation refused"
note "a private answer seen only by the person who pressed"
note "every documented example ran as written against a live server"
