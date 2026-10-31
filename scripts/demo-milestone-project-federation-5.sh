#!/usr/bin/env bash
#
# Project federation Milestone 5: the whole journey between two servers, in
# real browsers.
#
# Two servers built from this checkout, each serving the web client. The
# operator's part comes from docs/hosting.md: federation on, and A trusting B.
# The rest is docs/projects.md, done in a browser on each server — an
# administrator on A opens a project and adds somebody from B; she finds it on
# B, marked as living on A, and moves a card, which waits for A and lands; a
# move made stale by somebody on A is refused and put back, saying why; she
# follows a public project on A, reads it, asks to join and is let in; and when
# A stops answering, B says so.
#
# Both servers start afresh for each engine, since the last check stops A.
# WebKit cannot launch on every Linux distribution (it wants Debian's ICU); set
# ENGINES to include it where it can, as CI does on ubuntu-latest.
#
# Usage: scripts/demo-milestone-project-federation-5.sh   (SKIP_WEB_BUILD=1 to reuse a build)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT_A="${TDC_DEMO_PORT_A:-8441}"
PORT_B="${TDC_DEMO_PORT_B:-8442}"
# Where the databases are made: the development database by default, CI's
# service when it says so.
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
ENGINES="${ENGINES:-chromium firefox}"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"
SERVER_PIDS=()

stop_servers() {
  for pid in "${SERVER_PIDS[@]}"; do kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done
  SERVER_PIDS=()
}
cleanup() {
  stop_servers
  if [[ -z "${KEEP_DEMO:-}" ]]; then
    for name in a b; do
      psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS tdc_pf5_${name} WITH (FORCE);" >/dev/null 2>&1 || true
    done
    rm -rf "$WORK"
  else
    echo "kept: logs in $WORK"
  fi
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

say "Building the server and the web client from this checkout"
(cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
if [[ -z "${SKIP_WEB_BUILD:-}" || ! -f "$ROOT/apps/web/.output/public/index.html" ]]; then
  # Same-origin: an empty API base, or the build points the browser at whatever
  # happens to answer on the default port.
  (cd "$ROOT/apps/web" && NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
    || { tail -30 "$WORK/generate.log"; exit 1; }
fi

# start_server NAME PORT — a fresh database, the server on it, a founder.
start_server() {
  local name="$1" port="$2" database="tdc_pf5_$1"
  psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${database} WITH (FORCE);" >/dev/null
  psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${database};" >/dev/null
  # Plain HTTP, so the session cookie must not be marked Secure.
  TDC_DATABASE_URL="${DATABASE_SERVER}/${database}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${port}" \
  TDC_SERVER_CANONICAL_ADDRESS="http://${name}.localhost:${port}" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$ROOT/apps/web/.output/public" \
    "$WORK/tdc-server" >"$WORK/${name}.log" 2>&1 &
  SERVER_PIDS+=($!)
  for _ in $(seq 1 300); do
    curl -sf "http://localhost:${port}/api/health" >/dev/null 2>&1 && break
    sleep 1
  done
  curl -sf "http://localhost:${port}/api/health" >/dev/null || { tail -30 "$WORK/${name}.log"; exit 1; }
  curl -sf -X POST "http://localhost:${port}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null
}

# setting PORT NAME VALUE — as the founder, from the administration settings.
setting() {
  local token
  token="$(curl -sf -X POST "http://localhost:$1/api/auth/signin" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" | json 'd["token"]')"
  curl -sf -X PUT "http://localhost:$1/api/admin/settings/$2" -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' -d "{\"value\":\"$3\"}" >/dev/null
}

FAILED=0
for engine in $ENGINES; do
  say "In ${engine}: two servers, each with its own database and signing key"
  start_server a "$PORT_A"
  start_server b "$PORT_B"
  # Signed in once per server for all of these: sign-ins are limited.
  for port in "$PORT_A" "$PORT_B"; do
    setting "$port" federation.enabled true
    # Both servers are on this machine, which federation refuses by default.
    setting "$port" federation.allow_private_addresses true
  done
  setting "$PORT_A" federation.mode allowlist
  setting "$PORT_A" federation.allowed_servers "http://b.localhost:${PORT_B}"
  note "A on http://localhost:${PORT_A}, trusting B on http://localhost:${PORT_B}"

  say "In ${engine}: the journey, in a browser on each server"
  if ! (cd "$ROOT/apps/web" && \
    TDC_TEST_URL_A="http://localhost:${PORT_A}" TDC_TEST_URL_B="http://localhost:${PORT_B}" \
    TDC_CANONICAL_A="http://a.localhost:${PORT_A}" TDC_CANONICAL_B="http://b.localhost:${PORT_B}" \
    TDC_SERVER_A_PID="${SERVER_PIDS[0]}" TDC_FEDERATION_RUN="${engine}" \
    bunx playwright test -c playwright.projects.config.ts --project="federation-${engine}" --reporter=list); then
    FAILED=1
    tail -n 20 "$WORK/a.log" "$WORK/b.log" || true
  fi
  stop_servers
done

[[ "$FAILED" == 0 ]] || { printf '\n\033[31mProject federation Milestone 5 was not demonstrated\033[0m\n' >&2; exit 1; }
say "Project federation Milestone 5 demonstrated"
note "an administrator on A opened a project and added somebody from B, from the project's settings"
note "on B it was hers, marked as living on A; her move waited for A and landed on both"
note "a move made stale on A was refused and put back"
note "a public project on A was followed from B, read there, and joined once A let her in"
note "when A stopped, B said so"
