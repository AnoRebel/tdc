#!/usr/bin/env bash
#
# Calls Milestone 6: a six-person call through the forwarding unit, and a
# four-person mesh call without it.
#
# Six people, each in a Chromium context of their own with a fake camera and
# microphone. The mesh limit stays at its default of four. With calls.sfu off,
# four people call and each sends to each; with it on, six people call, and
# the fifth one's join moves the call to the unit built into the server.
#
# Everything runs inside a network namespace of its own (unshare, no root):
# the unit offers only addresses that are not loopback, as on a real host, so
# the namespace gives it one, and the database is reached through a socket
# bridged in.
#
# Usage: scripts/demo-milestone-calls-6.sh   (SKIP_WEB_BUILD=1 to reuse a build)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${TDC_DEMO_PORT:-8526}"
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
DATABASE="tdc_cb_calls6"
PASSWORD="correct horse battery staple"

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }

if [[ "${1:-}" == "--inside" ]]; then
  WORK="$2"
  ip link set lo up
  ip link add calls0 type dummy
  ip addr add 10.77.0.1/24 dev calls0
  ip link set calls0 up
  ip route add default dev calls0
  socat TCP-LISTEN:55433,bind=127.0.0.1,fork,reuseaddr "UNIX-CONNECT:$WORK/pg.sock" &
  bridge=$!

  cd "$WORK"
  TDC_DATABASE_URL="${DATABASE_SERVER}/${DATABASE}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${PORT}" \
  TDC_SERVER_CANONICAL_ADDRESS="http://localhost:${PORT}" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$WORK/web" \
  TDC_STORAGE_DIRECTORY="$WORK/attachments" \
    "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
  server=$!
  trap 'kill $server $bridge 2>/dev/null || true' EXIT
  for _ in $(seq 1 120); do
    curl -sf "http://localhost:${PORT}/api/health" >/dev/null 2>&1 && break
    sleep 1
  done
  curl -sf -X POST "http://localhost:${PORT}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null \
    || { tail -30 "$WORK/server.log"; exit 1; }
  # Five registrations: exactly what one address is allowed in an hour.
  for name in neema juma zawadi baraka amani; do
    curl -sf -X POST "http://localhost:${PORT}/api/auth/register" -H 'Content-Type: application/json' \
      -d "{\"username\":\"${name}\",\"password\":\"${PASSWORD}\"}" >/dev/null
  done
  token="$(curl -sf -X POST "http://localhost:${PORT}/api/auth/signin" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"
  curl -sf -X PUT "http://localhost:${PORT}/api/admin/features/calls" -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' -d '{"enabled": true}' >/dev/null

  cd "$ROOT/apps/web"
  TDC_TEST_URL="http://localhost:${PORT}" \
    bunx playwright test -c playwright.calls-b.config.ts --project=calls-6 --reporter=list \
    || { tail -n 20 "$WORK/server.log"; exit 1; }
  exit 0
fi

WORK="$(mktemp -d)"
cleanup() {
  [[ -n "${BRIDGE:-}" ]] && kill "$BRIDGE" 2>/dev/null || true
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

psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${DATABASE};" >/dev/null
socat "UNIX-LISTEN:$WORK/pg.sock,fork" "TCP:${DATABASE_SERVER##*@}" &
BRIDGE=$!

say "In a network namespace of its own: a mesh of four, then six through the forwarding unit"
unshare -rn "$0" --inside "$WORK" || {
  printf '\n\033[31mCalls Milestone 6 was not demonstrated\033[0m\n' >&2
  exit 1
}

say "Calls Milestone 6 demonstrated"
note "with calls.sfu off, four people held a video call, each hearing the other"
note "three over a direct connection to each of them"
note "with calls.sfu on, six people held a video call: the fifth join moved it to"
note "the forwarding unit, everybody was told, and each heard the other five over"
note "one connection, to the server (cameras arriving are counted in the log above)"
