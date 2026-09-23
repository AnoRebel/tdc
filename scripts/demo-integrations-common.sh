#!/usr/bin/env bash
#
# What every integrations milestone demonstration shares: building the server,
# starting one on a fresh database, signing in, and running the examples the
# documentation gives exactly as it gives them.
#
# Sourced, not run. Each demonstration sets PORT and DATABASE before sourcing.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"
SERVER_PIDS=""

cleanup() {
  for pid in ${SERVER_PIDS}; do kill "$pid" 2>/dev/null || true; done
  for pid in ${HELPER_PIDS:-}; do kill "$pid" 2>/dev/null || true; done
  # The databases are left for inspection unless asked otherwise; the
  # demonstration's own runs drop them first, so nothing accumulates.
  if [[ -n "${DROP_DATABASES:-}" ]]; then
    for database in ${DATABASES_MADE:-}; do
      docker exec tdc-database-1 psql -U tdc -d postgres \
        -c "DROP DATABASE IF EXISTS ${database} WITH (FORCE);" >/dev/null 2>&1 || true
    done
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
fail() {
  printf '  \033[31mFAILED:\033[0m %s\n' "$*" >&2
  exit 1
}

build_server() {
  say "Building the server"
  (cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
}

build_web() {
  say "Building the web client, served from the server's own origin"
  NUXT_PUBLIC_API_BASE="" bun run --cwd "$ROOT/apps/web" generate >"$WORK/web-build.log" 2>&1 ||
    { tail -20 "$WORK/web-build.log"; fail "the web client did not build"; }
}

# start_server NAME PORT [extra environment assignments...]
start_server() {
  local name="$1" port="$2"
  shift 2
  local database="tdc_int_${name}"

  docker exec tdc-database-1 psql -U tdc -d postgres \
    -c "DROP DATABASE IF EXISTS ${database} WITH (FORCE);" >/dev/null
  docker exec tdc-database-1 psql -U tdc -d postgres \
    -c "CREATE DATABASE ${database};" >/dev/null
  DATABASES_MADE="${DATABASES_MADE:-} ${database}"

  env \
    TDC_DATABASE_URL="postgres://tdc:tdc@127.0.0.1:55433/${database}?sslmode=disable" \
    TDC_SERVER_ADDRESS="localhost:${port}" \
    TDC_SERVER_CANONICAL_ADDRESS="http://localhost:${port}" \
    TDC_SERVER_SECURE_COOKIES=false \
    TDC_SERVER_WEB_ROOT="${WEB_ROOT:-}" \
    "$@" \
    "$WORK/tdc-server" >"$WORK/${name}.log" 2>&1 &
  SERVER_PIDS="${SERVER_PIDS} $!"

  for _ in $(seq 1 30); do
    curl -sf "http://localhost:${port}/api/server" >/dev/null 2>&1 && return 0
    sleep 1
  done
  tail -20 "$WORK/${name}.log" >&2
  fail "server ${name} did not start"
}

# setup_founder PORT — first-run setup, returning the founder's token.
setup_founder() {
  local base="http://localhost:$1"
  curl -sf -X POST "${base}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null ||
    fail "first-run setup was refused"
  sign_in "$1" founder
}

# sign_in PORT USERNAME — prints a session token.
sign_in() {
  curl -sf -X POST "http://localhost:$1/api/auth/signin" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$2\",\"password\":\"${PASSWORD}\"}" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])'
}

# register PORT USERNAME — registers and prints a session token.
register() {
  curl -sf -X POST "http://localhost:$1/api/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$2\",\"password\":\"${PASSWORD}\"}" >/dev/null ||
    fail "registering $2 was refused"
  sign_in "$1" "$2"
}

# api PORT TOKEN METHOD PATH [BODY] — prints the body, then the status on the
# last line, so a caller can assert on both.
api() {
  local port="$1" token="$2" method="$3" path="$4" body="${5:-}"
  local args=(-s -X "$method" "http://localhost:${port}${path}" -w '\n%{http_code}')
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer ${token}")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' -d "$body")
  curl "${args[@]}"
}

# status_of OUTPUT — the status line api printed last.
status_of() { tail -n1 <<<"$1"; }
# body_of OUTPUT — everything api printed before the status.
body_of() { sed '$d' <<<"$1"; }

# json FIELD — reads one field from the JSON on stdin.
json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

# expect_status OUTPUT WANT WHAT
expect_status() {
  local got
  got="$(status_of "$1")"
  [[ "$got" == "$2" ]] || fail "$3: got ${got}, want $2 — $(body_of "$1" | head -c 300)"
  note "$3: $2"
}

setting() {
  local port="$1" token="$2" key="$3" value="$4"
  local out
  out="$(api "$port" "$token" PUT "/api/admin/settings/${key}" "{\"value\":\"${value}\"}")"
  [[ "$(status_of "$out")" == "204" ]] || fail "setting ${key}=${value}: $(body_of "$out")"
}

# doc_example NAME PORT — prints the fenced block the documentation marks with
# <!-- demo: NAME -->, pointed at this server instead of the documented one.
#
# The example is run as written, so a documented command that stops working
# fails the demonstration rather than going stale unnoticed.
doc_example() {
  local name="$1" port="$2" file="${3:-$ROOT/docs/integrating.md}"
  awk -v marker="<!-- demo: ${name} -->" '
    $0 == marker { armed = 1; next }
    armed && /^```/ { if (inside) exit; inside = 1; next }
    inside { print }
  ' "$file" | sed "s#http://localhost:8080#http://localhost:${port}#g"
}

# run_doc_example NAME PORT — runs it with curl reporting each status.
run_doc_example() {
  local script
  script="$(doc_example "$1" "$2" "${3:-$ROOT/docs/integrating.md}")"
  [[ -n "$script" ]] || fail "the documentation has no example marked ${1}"
  bash -c "curl() { command curl -sS -w '\nHTTP %{http_code}\n' \"\$@\"; }; set -e; ${script}"
}

# expect_doc_statuses OUTPUT — every request the example made succeeded.
expect_doc_statuses() {
  local statuses
  statuses="$(grep -E '^HTTP [0-9]{3}$' <<<"$1" | awk '{print $2}' | tr '\n' ' ')"
  [[ -n "$statuses" ]] || fail "$2: the example made no request"
  for code in $statuses; do
    [[ "$code" =~ ^2 ]] || fail "$2: the documented example answered ${code} — $(head -c 300 <<<"$1")"
  done
  note "$2: ${statuses}"
}
