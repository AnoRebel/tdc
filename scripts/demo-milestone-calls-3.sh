#!/usr/bin/env bash
#
# Calls Milestone 3: a protected call link with a lobby, in real browsers.
#
# One server built from this checkout, serving the web client, with calls on
# and guests allowed, as docs/hosting.md says. The rest is docs/using-tdc.md,
# done in Chromium with a fake camera and microphone: in a call in a private
# channel, the founder makes a link with a passcode and a lobby; somebody
# signed in elsewhere on the server opens it, waits, and is let in; so is a
# guest with no account; and neither can read the channel.
#
# Usage: scripts/demo-milestone-calls-2.sh   (SKIP_WEB_BUILD=1 to reuse a build)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${TDC_DEMO_PORT:-8512}"
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
DATABASE="tdc_ca_m3"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"
SERVER=""

cleanup() {
  [[ -n "$SERVER" ]] && { kill "$SERVER" 2>/dev/null || true; wait "$SERVER" 2>/dev/null || true; }
  psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null 2>&1 || true
  if [[ -z "${KEEP_DEMO:-}" ]]; then rm -rf "$WORK"; else echo "kept: logs in $WORK"; fi
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

say "Building the server and the web client from this checkout"
(cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
if [[ -z "${SKIP_WEB_BUILD:-}" || ! -f "$ROOT/apps/web/.output/public/index.html" ]]; then
  # Same-origin: an empty API base.
  (cd "$ROOT/apps/web" && NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
    || { tail -30 "$WORK/generate.log"; exit 1; }
fi

say "A server on a fresh database"
psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${DATABASE};" >/dev/null
# Plain HTTP, so the session cookie must not be marked Secure.
TDC_DATABASE_URL="${DATABASE_SERVER}/${DATABASE}?sslmode=disable" \
TDC_SERVER_ADDRESS="localhost:${PORT}" \
TDC_SERVER_CANONICAL_ADDRESS="http://localhost:${PORT}" \
TDC_SERVER_SECURE_COOKIES=false \
TDC_SERVER_WEB_ROOT="$ROOT/apps/web/.output/public" \
  "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
SERVER=$!
for _ in $(seq 1 120); do
  curl -sf "http://localhost:${PORT}/api/health" >/dev/null 2>&1 && break
  sleep 1
done
curl -sf "http://localhost:${PORT}/api/health" >/dev/null || { tail -30 "$WORK/server.log"; exit 1; }
curl -sf -X POST "http://localhost:${PORT}/api/setup" -H 'Content-Type: application/json' \
  -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null

say "The operator turns calls on, and lets guests join calls through links"
TOKEN="$(curl -sf -X POST "http://localhost:${PORT}/api/auth/signin" -H 'Content-Type: application/json' \
  -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" | json 'd["token"]')"
curl -sf -X PUT "http://localhost:${PORT}/api/admin/features/calls" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d '{"enabled":true}' >/dev/null
curl -sf -X PUT "http://localhost:${PORT}/api/admin/settings/calls.guests" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d '{"value":"true"}' >/dev/null
note "on http://localhost:${PORT}"

say "The milestone, in Chromium"
if ! (cd "$ROOT/apps/web" && TDC_TEST_URL="http://localhost:${PORT}" \
  bunx playwright test -c playwright.calls-a.config.ts milestones/calls-milestone-3.spec.ts --reporter=list); then
  tail -n 30 "$WORK/server.log" || true
  printf '\n\033[31mCalls Milestone 3 was not demonstrated\033[0m\n' >&2
  exit 1
fi

say "Calls Milestone 3 demonstrated"
note "in a call in a private channel, the founder made a link with a passcode, a lobby and guests allowed"
note "Kito, signed in on the server but not in the channel, gave the passcode, waited, and was let in"
note "Wanjiru, with no account, joined as a guest the same way, and was shown to the call as a guest"
note "both saw the founder's camera; neither could read the channel, its messages or its members"
