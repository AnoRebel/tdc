#!/usr/bin/env bash
#
# What the directory milestone demonstrations share: a server of their own on a
# fresh database, people signed in over the API, and assertions that stop the
# demonstration the moment a claim does not hold.
#
# Sourced, not run. Each demonstration sets DEMO_NAME and DEMO_PORT first.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"
DATABASE="tdc_dir_${DEMO_NAME}"
BASE="http://localhost:${DEMO_PORT}"

cleanup() {
  [[ -n "${SERVER_PID:-}" ]] && kill "$SERVER_PID" 2>/dev/null || true
  docker exec tdc-database-1 psql -U tdc -d postgres \
    -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
fail() { printf '  \033[31mFAILED: %s\033[0m\n' "$*" >&2; exit 1; }
pass() { printf '  \033[32mok\033[0m %s\n' "$*"; }

# Builds the server and, unless told the build is current, the web application
# it serves — with an empty API base, so the page calls the host it came from.
build() {
  say "Building the server and the web application"
  (cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
  if [[ -z "${TDC_DEMO_SKIP_GENERATE:-}" ]]; then
    (cd "$ROOT/apps/web" && NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
      || fail "the web application did not build; see $WORK/generate.log"
  fi
}

# Starts the server on its own database. A hostname rather than an IP for the
# canonical address, which passkey configuration refuses.
start() {
  docker exec tdc-database-1 psql -U tdc -d postgres \
    -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
  docker exec tdc-database-1 psql -U tdc -d postgres -c "CREATE DATABASE ${DATABASE};" >/dev/null

  TDC_DATABASE_URL="postgres://tdc:tdc@127.0.0.1:55433/${DATABASE}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${DEMO_PORT}" \
  TDC_SERVER_CANONICAL_ADDRESS="http://d.localhost:${DEMO_PORT}" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$ROOT/apps/web/.output/public" \
    "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
  SERVER_PID=$!

  for _ in $(seq 1 30); do
    curl -sf "$BASE/api/server" >/dev/null 2>&1 && return 0
    sleep 1
  done
  fail "the server did not start; see $WORK/server.log"
}

# api METHOD PATH TOKEN [BODY] — prints the body; the status goes to $WORK/status.
api() {
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-s -o "$WORK/body" -w '%{http_code}' -X "$method" "$BASE$path")
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' -d "$body")
  curl "${args[@]}" >"$WORK/status"
  cat "$WORK/body"
}

status() { cat "$WORK/status"; }

# expect_status WANT DESCRIPTION — after an api call.
expect_status() {
  local got
  got="$(status)"
  [[ "$got" == "$1" ]] || fail "$2: status $got, want $1: $(cat "$WORK/body")"
  pass "$2"
}

# json EXPRESSION — evaluates a Python expression over the last body, bound to d.
json() {
  python3 -c "import json,sys; d=json.load(open('$WORK/body')); print($1)"
}

# check EXPRESSION DESCRIPTION — the expression must be true of the last body.
check() {
  local result
  result="$(python3 -c "import json,sys; d=json.load(open('$WORK/body')); print(bool($1))")"
  [[ "$result" == "True" ]] || fail "$2: $(head -c 400 "$WORK/body")"
  pass "$2"
}

signin() {
  api POST /api/auth/signin "" "{\"username\":\"$1\",\"password\":\"$PASSWORD\"}" >/dev/null
  [[ "$(status)" == "200" ]] || fail "signing in as $1: $(cat "$WORK/body")"
  json 'd["token"]'
}

register() {
  api POST /api/auth/register "" "{\"username\":\"$1\",\"password\":\"$PASSWORD\"}" >/dev/null
  [[ "$(status)" == "201" ]] || fail "registering $1: $(cat "$WORK/body")"
  signin "$1"
}

# The founder, and the directory switched on as the hosting guide says.
founder_with_directory() {
  api POST /api/setup "" "{\"username\":\"founder\",\"password\":\"$PASSWORD\"}" >/dev/null
  FOUNDER="$(signin founder)"
  api PUT /api/admin/features/directory "$FOUNDER" '{"enabled":true}' >/dev/null
  expect_status 204 "the operator turns the directory on"
}

# grant_role HOLDER_TOKEN PERMISSION... — a new role holding the permissions.
grant_role() {
  local holder="$1"; shift
  api POST /api/admin/roles "$FOUNDER" "{\"name\":\"role-$RANDOM$RANDOM\"}" >/dev/null
  local role
  role="$(json 'd["id"]')"
  for permission in "$@"; do
    api PUT "/api/admin/roles/$role/permissions" "$FOUNDER" \
      "{\"permission\":\"$permission\",\"granted\":true}" >/dev/null
    [[ "$(status)" == "204" ]] || fail "granting $permission"
  done
  api GET /api/me "$holder" >/dev/null
  local id
  id="$(json 'd["id"]')"
  api POST "/api/admin/roles/$role/members" "$FOUNDER" "{\"user\":\"$id\"}" >/dev/null
  [[ "$(status)" == "204" ]] || fail "assigning the role"
}

# Runs the browser half against this server.
browser() {
  (cd "$ROOT/apps/web" && TDC_TEST_URL="$BASE" bunx playwright test --project=directory "$@") \
    || fail "the browser checks did not pass"
}
