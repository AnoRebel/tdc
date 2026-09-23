#!/usr/bin/env bash
#
# What every projects milestone demonstration shares: a real server, built from
# this checkout, on its own database, serving the web client it was built with.
#
# Sourced, not run. Each demonstration sets DEMO_NAME and DEMO_PORT, sources
# this, and then says what it demonstrates — every claim asserted with check,
# which exits non-zero the moment a claim is false, so a demonstration that
# prints its way to the end has actually shown what it says.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PASSWORD="correct horse battery staple"
DEMO_PORT="${DEMO_PORT:-8401}"
DATABASE="tdc_prj_${DEMO_NAME:-demo}"
# A hostname rather than an address: passkey configuration refuses an IP, and
# the server refuses to start with one as its canonical address.
BASE="http://localhost:${DEMO_PORT}"
CANONICAL="http://p.localhost:${DEMO_PORT}"
WORK="$(mktemp -d)"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]]; then kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; fi
  if [[ -z "${KEEP_DEMO:-}" ]]; then
    docker exec tdc-database-1 psql -U tdc -d postgres -q \
      -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null 2>&1 || true
    rm -rf "$WORK"
  else
    echo "kept: database ${DATABASE}, work directory ${WORK}"
  fi
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }

# check DESCRIPTION COMMAND... — runs the command; its failure fails the demo.
check() {
  local description="$1"
  shift
  if "$@"; then
    printf '  \033[32mok\033[0m  %s\n' "$description"
  else
    printf '  \033[31mFAILED\033[0m  %s\n' "$description" >&2
    exit 1
  fi
}

# json EXPRESSION — reads JSON on stdin and prints a Python expression over it
# as `d`.
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

build() {
  say "Building the server and the web client from this checkout"
  (cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
  if [[ -z "${SKIP_WEB_BUILD:-}" || ! -f "$ROOT/apps/web/.output/public/index.html" ]]; then
    # Same-origin: an empty API base, or the build points the browser at
    # whatever happens to answer on the default port.
    (cd "$ROOT/apps/web" && NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
      || { tail -30 "$WORK/generate.log"; exit 1; }
  fi
  note "server and web client built"
}

start_server() {
  docker exec tdc-database-1 psql -U tdc -d postgres -q \
    -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
  docker exec tdc-database-1 psql -U tdc -d postgres -q -c "CREATE DATABASE ${DATABASE};" >/dev/null

  # Plain HTTP here, so the session cookie must not be marked Secure or the
  # browser keeps it and never sends it back.
  TDC_DATABASE_URL="postgres://tdc:tdc@127.0.0.1:55433/${DATABASE}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${DEMO_PORT}" \
  TDC_SERVER_CANONICAL_ADDRESS="${CANONICAL}" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$ROOT/apps/web/.output/public" \
    "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
  SERVER_PID=$!

  for _ in $(seq 1 60); do
    curl -sf "${BASE}/api/health" >/dev/null 2>&1 && break
    sleep 0.5
  done
  curl -sf "${BASE}/api/health" >/dev/null || { tail -40 "$WORK/server.log"; exit 1; }

  curl -sf -X POST "${BASE}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null
  note "server on ${BASE}, database ${DATABASE}, founder account made"
}

# sign_in USERNAME — prints a bearer token.
sign_in() {
  curl -sf -X POST "${BASE}/api/auth/signin" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"${PASSWORD}\"}" | json 'd["token"]'
}

# register USERNAME — makes an ordinary account.
register() {
  curl -sf -X POST "${BASE}/api/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"${PASSWORD}\"}" >/dev/null
}

# api TOKEN METHOD PATH [BODY] — prints the body; fails on an error status.
api() {
  local token="$1" method="$2" path="$3" body="${4:-}"
  if [[ -n "$body" ]]; then
    curl -sf -X "$method" "${BASE}${path}" -H "Authorization: Bearer ${token}" \
      -H 'Content-Type: application/json' -d "$body"
  else
    curl -sf -X "$method" "${BASE}${path}" -H "Authorization: Bearer ${token}"
  fi
}

# status TOKEN METHOD PATH [BODY] — prints only the HTTP status.
status() {
  local token="$1" method="$2" path="$3" body="${4:-}"
  curl -s -o /dev/null -w '%{http_code}' -X "$method" "${BASE}${path}" \
    -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
    ${body:+-d "$body"}
}

# browser SPEC — drives the real web client through a Playwright spec.
browser() {
  (cd "$ROOT/apps/web" && TDC_TEST_URL="$BASE" \
    bunx playwright test -c playwright.projects.config.ts "$1" --reporter=list)
}
