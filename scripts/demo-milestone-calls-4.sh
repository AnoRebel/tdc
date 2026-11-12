#!/usr/bin/env bash
#
# Calls Milestone 4: calls across servers, in real browsers.
#
# Two servers built from this checkout, each serving the web client. The
# operator's part comes from docs/hosting.md: federation on, each trusting the
# other, calls on, and calls across servers on. The rest is docs/using-tdc.md,
# in Chromium with a fake camera and microphone: a person on A rings a person
# on B by his address and he answers with his camera; with B's federated
# calling off, with his own setting refusing other servers, and with A's
# federated calling off, the ring never reaches him.
#
# Usage: scripts/demo-milestone-calls-4.sh   (SKIP_WEB_BUILD=1 to reuse a build)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT_A="${TDC_DEMO_PORT_A:-8513}"
PORT_B="${TDC_DEMO_PORT_B:-8514}"
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"
SERVER_PIDS=()

cleanup() {
  for pid in "${SERVER_PIDS[@]}"; do kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done
  for name in a b; do
    psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS tdc_ca_m4_${name} WITH (FORCE);" >/dev/null 2>&1 || true
  done
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

# start_server NAME PORT — a fresh database, the server on it, a founder.
start_server() {
  local name="$1" port="$2" database="tdc_ca_m4_$1"
  psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${database} WITH (FORCE);" >/dev/null 2>&1
  psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${database};" >/dev/null
  # Plain HTTP, so the session cookie must not be marked Secure.
  TDC_DATABASE_URL="${DATABASE_SERVER}/${database}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${port}" \
  TDC_SERVER_CANONICAL_ADDRESS="http://${name}.localhost:${port}" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$ROOT/apps/web/.output/public" \
    "$WORK/tdc-server" >"$WORK/${name}.log" 2>&1 &
  SERVER_PIDS+=($!)
  for _ in $(seq 1 120); do
    curl -sf "http://localhost:${port}/api/health" >/dev/null 2>&1 && break
    sleep 1
  done
  curl -sf "http://localhost:${port}/api/health" >/dev/null || { tail -30 "$WORK/${name}.log"; exit 1; }
  curl -sf -X POST "http://localhost:${port}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null
}

# operate PORT OTHER_PORT OTHER_NAME — as the founder, from the hosting guide.
operate() {
  local port="$1" token
  token="$(curl -sf -X POST "http://localhost:${port}/api/auth/signin" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" | json 'd["token"]')"
  set_it() {
    curl -sf -X PUT "http://localhost:${port}/api/admin/$1" -H "Authorization: Bearer ${token}" \
      -H 'Content-Type: application/json' -d "$2" >/dev/null
  }
  set_it settings/federation.enabled '{"value":"true"}'
  # Both servers are on this machine, which federation refuses by default.
  set_it settings/federation.allow_private_addresses '{"value":"true"}'
  set_it settings/federation.mode '{"value":"allowlist"}'
  set_it settings/federation.allowed_servers "{\"value\":\"http://$3.localhost:$2\"}"
  set_it features/calls '{"enabled":true}'
  set_it settings/calls.federated '{"value":"true"}'
}

say "Two servers, each with its own database and signing key"
start_server a "$PORT_A"
start_server b "$PORT_B"
operate "$PORT_A" "$PORT_B" b
operate "$PORT_B" "$PORT_A" a
note "A on http://localhost:${PORT_A} and B on http://localhost:${PORT_B}, trusting each other, calls across servers on"

say "The milestone, in Chromium, a browser on each server"
if ! (cd "$ROOT/apps/web" && \
  TDC_TEST_URL_A="http://localhost:${PORT_A}" TDC_TEST_URL_B="http://localhost:${PORT_B}" \
  TDC_CANONICAL_A="http://a.localhost:${PORT_A}" TDC_CANONICAL_B="http://b.localhost:${PORT_B}" \
  bunx playwright test -c playwright.calls-a.config.ts milestones/calls-milestone-4.spec.ts --reporter=list); then
  grep -v '"msg":"request"' "$WORK/a.log" | tail -n 15 || true
  grep -v '"msg":"request"' "$WORK/b.log" | tail -n 15 || true
  printf '\n\033[31mCalls Milestone 4 was not demonstrated\033[0m\n' >&2
  exit 1
fi

say "Calls Milestone 4 demonstrated"
note "Asha on A rang Bakari on B by his address; he answered with his camera and they saw each other"
note "with B's federated calling off, the ring never reached him"
note "with his own setting refusing other servers, the ring never reached him"
note "with A's federated calling off, A offered no address to ring and refused one asked for"
note "with everything back on, it rang him — the only ring he had had since the first"
