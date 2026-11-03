#!/usr/bin/env bash
#
# Calls Milestone 7, first half: a call that steps down to sound on a
# throttled connection and recovers, in Chromium with fake camera and
# microphone.
#
# Browser network emulation does not touch WebRTC, so the throttle is the
# kernel's: everything runs inside a network namespace of its own (unshare,
# no root needed), where loopback UDP — the call's media — is limited with tc
# and then freed. The database is reached through a socket bridged out of the
# namespace.
#
# The second half, a call between the web client and the desktop client, is
# scripts/demo-milestone-calls-7-desktop.sh.
#
# Usage: scripts/demo-milestone-calls-7.sh   (SKIP_WEB_BUILD=1 to reuse a build)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${TDC_DEMO_PORT:-8471}"
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
DATABASE="tdc_calls7"
PASSWORD="correct horse battery staple"

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }

# Inside the namespace: loopback up, an address WebRTC will gather (it skips
# loopback ones, and any interface without a route out), the database bridged
# in, the server, and the browsers.
if [[ "${1:-}" == "--inside" ]]; then
  WORK="$2"
  ip link set lo up
  ip link add calls0 type dummy
  ip addr add 10.77.0.1/24 dev calls0
  ip link set calls0 up
  ip route add default dev calls0
  socat TCP-LISTEN:55433,bind=127.0.0.1,fork,reuseaddr "UNIX-CONNECT:$WORK/pg.sock" &
  sleep 0.5

  TDC_DATABASE_URL="${DATABASE_SERVER}/${DATABASE}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${PORT}" \
  TDC_SERVER_CANONICAL_ADDRESS="http://localhost:${PORT}" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$ROOT/apps/web/.output/public" \
    "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
  server=$!
  trap 'kill $server 2>/dev/null || true' EXIT
  for _ in $(seq 1 120); do
    curl -sf "http://localhost:${PORT}/api/health" >/dev/null 2>&1 && break
    sleep 1
  done
  curl -sf -X POST "http://localhost:${PORT}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null \
    || { tail -30 "$WORK/server.log"; exit 1; }

  cd "$ROOT/apps/web"
  TDC_TEST_URL="http://localhost:${PORT}" \
    bunx playwright test -c playwright.projects.config.ts --project=calls-throttled --reporter=list \
    || { tail -20 "$WORK/server.log"; exit 1; }
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
  (cd "$ROOT/apps/web" && NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
    || { tail -30 "$WORK/generate.log"; exit 1; }
fi

psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${DATABASE};" >/dev/null
# The database, as a socket the namespace can reach.
socat "UNIX-LISTEN:$WORK/pg.sock,fork" "TCP:${DATABASE_SERVER##*@}" &
BRIDGE=$!

say "In a network namespace of its own: a call, throttled to 100 kbit/s, then freed"
unshare -rn "$0" --inside "$WORK" || {
  printf '\n\033[31mCalls Milestone 7 (throttled connection) was not demonstrated\033[0m\n' >&2
  exit 1
}

say "Calls Milestone 7, throttled connection, demonstrated"
note "two people held a video call; their connection was cut to 100 kbit/s"
note "the camera gave way and the call went on, with the founder told why"
note "freed, the camera came back without anybody doing anything"
