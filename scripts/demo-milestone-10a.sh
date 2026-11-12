#!/usr/bin/env bash
#
# The Milestone 10a demonstration.
#
# A program authenticates with a token, posts a message offering two choices, a
# person presses one in the web client and another in the desktop client, and
# the program receives both presses and replies.
#
# The two presses are made where the two clients actually live. The web press
# is a click in a real browser, driven by Playwright. The desktop press goes
# through the Go service the Wails binding calls, because the desktop does not
# speak HTTP — driving a browser against its built frontend would exercise a
# transport it never uses and prove the wrong thing convincingly.
#
# Usage: scripts/demo-milestone-10a.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${TDC_DEMO_PORT:-8104}"
BASE="http://127.0.0.1:${PORT}"
DATABASE="${TDC_DEMO_DATABASE:-tdc_milestone_10a}"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"

cleanup() {
  [[ -n "${SERVER_PID:-}" ]] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }

say "Building the server and the web client"
(cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
NUXT_PUBLIC_API_BASE="" bun run --cwd "$ROOT/apps/web" generate >/dev/null

say "A fresh database, so the demonstration starts from nothing"
docker exec tdc-database-1 psql -U tdc -d postgres \
  -c "DROP DATABASE IF EXISTS ${DATABASE};" >/dev/null
docker exec tdc-database-1 psql -U tdc -d postgres \
  -c "CREATE DATABASE ${DATABASE};" >/dev/null

TDC_DATABASE_URL="postgres://tdc:tdc@127.0.0.1:55433/${DATABASE}?sslmode=disable" \
TDC_SERVER_WEB_ROOT="$ROOT/apps/web/.output/public" \
TDC_SERVER_ADDRESS="127.0.0.1:${PORT}" \
  "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 30); do
  curl -sf "$BASE/api/server" >/dev/null 2>&1 && break
  sleep 1
done

say "An account, so there is somebody to press a button"
curl -sf -X POST "$BASE/api/setup" -H 'Content-Type: application/json' \
  -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null

say "The web client: a program asks, and a person presses Deploy"
(cd "$ROOT/apps/web" && TDC_TEST_URL="$BASE" \
  bunx playwright test --project=milestone-10a --reporter=list) | tee "$WORK/web.out"

# What the web half created, so the desktop presses the same question.
#
# Read rather than eval'd: an event identifier begins with "$", and eval would
# expand it as a variable — which it did, and the script failed on a name that
# was really half a message identifier.
value_of() {
  grep -E "^$1=" "$WORK/web.out" | tail -1 | cut -d= -f2- | tr -d '\r'
}
MILESTONE_ROOM="$(value_of MILESTONE_ROOM)"
MILESTONE_MESSAGE="$(value_of MILESTONE_MESSAGE)"
MILESTONE_SLUG="$(value_of MILESTONE_SLUG)"

if [[ -z "$MILESTONE_ROOM" || -z "$MILESTONE_MESSAGE" ]]; then
  echo "The web half did not report what it created." >&2
  exit 1
fi

say "The desktop client: the other choice, through the Wails service"
(cd "$ROOT/apps/desktop" &&
  TDC_MILESTONE_SERVER="$BASE" \
  TDC_MILESTONE_ROOM="$MILESTONE_ROOM" \
  TDC_MILESTONE_MESSAGE="$MILESTONE_MESSAGE" \
  TDC_MILESTONE_USER="founder" \
  TDC_MILESTONE_PASSWORD="$PASSWORD" \
  go test ./internal/api/ -run TestMilestone10aDesktopPressReachesTheProgram -v -count=1)

say "What the channel holds afterwards"
TOKEN=$(curl -sf -X POST "$BASE/api/auth/signin" -H 'Content-Type: application/json' \
  -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')

curl -s "$BASE/api/rooms/${MILESTONE_ROOM}/timeline" -H "Authorization: Bearer $TOKEN" |
  python3 -c '
import json, sys
for event in json.load(sys.stdin):
    content = event.get("content", {})
    said = content.get("body") or content.get("action", "")
    who = event["sender"].split("@")[0]
    kind = event["type"]
    print("  {:16} {:10} {}".format(kind, who, said))
'

say "Both presses reached the program, from two clients, on one question."
