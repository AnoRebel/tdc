#!/usr/bin/env bash
#
# Calls Milestone 5: one person presents a browser tab, another records with
# everybody told, the recording appears in the conversation, and the
# conversation and the audit log show who recorded and when.
#
# The operator's part comes from docs/hosting.md: calls on, recording on
# (calls.recording), and call.record — which no role holds until somebody is
# given it — granted through a role of its own to the one person who records.
# The rest is docs/using-tdc.md, done in Chromium (the full browser, headless,
# with fake camera and microphone): neema presents a tab titled "Agenda", juma
# records, and the founder reads the audit log.
#
# Usage: scripts/demo-milestone-calls-5.sh   (SKIP_WEB_BUILD=1 to reuse a build)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${TDC_DEMO_PORT:-8525}"
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
DATABASE="tdc_cb_calls5"
PASSWORD="correct horse battery staple"
URL="http://localhost:${PORT}"
WORK="$(mktemp -d)"
SERVER=""

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

cleanup() {
  [[ -n "$SERVER" ]] && { kill "$SERVER" 2>/dev/null || true; wait "$SERVER" 2>/dev/null || true; }
  psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null 2>&1 || true
  if [[ -z "${KEEP_DEMO:-}" ]]; then rm -rf "$WORK"; else echo "kept: logs in $WORK"; fi
}
trap cleanup EXIT

say "Building the server and the web client from this checkout"
(cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
if [[ -z "${SKIP_WEB_BUILD:-}" || ! -f "$ROOT/apps/web/.output/public/index.html" ]]; then
  (cd "$ROOT/apps/web" && flock /tmp/tdc-web-build.lock env NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
    || { tail -30 "$WORK/generate.log"; exit 1; }
fi
# A copy, taken while nobody is building: the checkout's build may be
# regenerated while this runs, and a page holding the old one's chunk names
# would break.
flock /tmp/tdc-web-build.lock cp -r "$ROOT/apps/web/.output/public" "$WORK/web"

say "A server on a database of its own"
psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${DATABASE};" >/dev/null
(
  cd "$WORK"
  TDC_DATABASE_URL="${DATABASE_SERVER}/${DATABASE}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${PORT}" \
  TDC_SERVER_CANONICAL_ADDRESS="$URL" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$WORK/web" \
  TDC_STORAGE_DIRECTORY="$WORK/attachments" \
    exec "$WORK/tdc-server" >"$WORK/server.log" 2>&1
) &
SERVER=$!
for _ in $(seq 1 120); do
  curl -sf "$URL/api/health" >/dev/null 2>&1 && break
  sleep 1
done
curl -sf "$URL/api/health" >/dev/null || { tail -30 "$WORK/server.log"; exit 1; }
curl -sf -X POST "$URL/api/setup" -H 'Content-Type: application/json' \
  -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null

say "The operator: calls on, recording on, and juma allowed to record"
TOKEN="$(curl -sf -X POST "$URL/api/auth/signin" -H 'Content-Type: application/json' \
  -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" | json 'd["token"]')"
api() {
  local method="$1" path="$2" body="${3:-}"
  curl -sf -X "$method" "$URL$path" -H "Authorization: Bearer ${TOKEN}" \
    -H 'Content-Type: application/json' ${body:+-d "$body"}
}
api PUT /api/admin/features/calls '{"enabled": true}' >/dev/null
api PUT /api/admin/settings/calls.recording '{"value": "true"}' >/dev/null
for name in neema juma; do
  curl -sf -X POST "$URL/api/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"${name}\",\"password\":\"${PASSWORD}\"}" >"$WORK/${name}.json"
done
JUMA="$(json 'd["id"]' <"$WORK/juma.json")"
ROLE="$(api POST /api/admin/roles '{"name": "Recorders", "description": "May record calls"}' | json 'd["id"]')"
api PUT "/api/admin/roles/${ROLE}/permissions" '{"permission": "call.record", "granted": true}' >/dev/null
api POST "/api/admin/roles/${ROLE}/members" "{\"user\": \"${JUMA}\"}" >/dev/null
note "calls and calls.recording on; a role Recorders holding call.record, given to juma"

say "In Chromium: neema presents a tab, juma records it"
if ! (cd "$ROOT/apps/web" && TDC_TEST_URL="$URL" \
  bunx playwright test -c playwright.calls-b.config.ts --project=calls-5 --reporter=list); then
  tail -n 20 "$WORK/server.log"
  printf '\n\033[31mCalls Milestone 5 was not demonstrated\033[0m\n' >&2
  exit 1
fi

say "Calls Milestone 5 demonstrated"
note "neema shared a browser tab (Agenda) and it took the stage on juma's screen"
note "juma started recording: neema was told, by name, before his device recorded"
note "the recording was saved to the conversation as an attachment"
note "the conversation shows juma started and stopped recording, and for how long;"
note "the audit log shows call.recording.started and .stopped by juma, with the time"
