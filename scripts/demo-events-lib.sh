#!/usr/bin/env bash
#
# What every events milestone demonstration shares: a real server, built from
# this checkout, on a database of its own, serving the web client, with a
# founder; and a way to make many people without tripping the server's own
# limit of five registrations an hour from one address.
#
# Sourced, not run. Each demonstration sets DEMO_NAME and DEMO_PORT, sources
# this, and then says what it demonstrates. Every claim is asserted — in the
# Playwright spec, or here with check — so a demonstration that prints its way
# to the end has shown what it says.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PASSWORD="correct horse battery staple"
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
DATABASE="tdc_ev_${DEMO_NAME}"
BASE="http://localhost:${DEMO_PORT}"
# The API is reached by address, so a request can be sent from a chosen
# loopback address (see register).
API="http://127.0.0.1:${DEMO_PORT}"
WORK="$(mktemp -d)"
SERVER_PID=""
EXTRA_PIDS=()

cleanup() {
  for pid in "${EXTRA_PIDS[@]}" "$SERVER_PID"; do
    [[ -n "$pid" ]] && { kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; }
  done
  if [[ -z "${KEEP_DEMO:-}" ]]; then
    psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null 2>&1 || true
    rm -rf "$WORK"
  else
    echo "kept: database ${DATABASE}, work directory ${WORK}"
  fi
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
fail() { printf '\n\033[31m%s\033[0m\n' "$*" >&2; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

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

build() {
  say "Building the server and the web client from this checkout"
  (cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
  if [[ -z "${SKIP_WEB_BUILD:-}" || ! -f "$ROOT/apps/web/.output/public/index.html" ]]; then
    # Same-origin: an empty API base, or the build points the browser at
    # whatever happens to answer on the default port.
    (cd "$ROOT/apps/web" && flock /tmp/tdc-web-build.lock env NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
      || { tail -30 "$WORK/generate.log"; exit 1; }
  fi
  # A copy of its own to serve: the build in apps/web is shared, and a rebuild
  # started by somebody else part way through a demonstration would empty it
  # under the running server. Copied while no build holds the lock.
  flock -s /tmp/tdc-web-build.lock cp -a "$ROOT/apps/web/.output/public" "$WORK/public"
  [[ -f "$WORK/public/index.html" ]] || fail "the web client build has no index.html"
  note "server and web client built"
}

# start_server [ENV=VALUE...] — a fresh database, the server on it, a founder.
start_server() {
  psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
  psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${DATABASE};" >/dev/null
  # Plain HTTP, so the session cookie must not be marked Secure or the
  # browser keeps it and never sends it back.
  env TDC_DATABASE_URL="${DATABASE_SERVER}/${DATABASE}?sslmode=disable" \
    TDC_SERVER_ADDRESS="127.0.0.1:${DEMO_PORT}" \
    TDC_SERVER_CANONICAL_ADDRESS="${BASE}" \
    TDC_SERVER_SECURE_COOKIES=false \
    TDC_SERVER_WEB_ROOT="$WORK/public" \
    "$@" \
    "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
  SERVER_PID=$!
  # Migrating a fresh database can take minutes on a busy machine.
  for _ in $(seq 1 300); do
    curl -sf "${API}/api/health" >/dev/null 2>&1 && break
    kill -0 "$SERVER_PID" 2>/dev/null || break
    sleep 1
  done
  curl -sf "${API}/api/health" >/dev/null || { tail -40 "$WORK/server.log"; exit 1; }
  curl -sf -X POST "${API}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null
  FOUNDER="$(sign_in founder)"
  note "server on ${BASE}, database ${DATABASE}, founder account made"
}

# Registration is limited to five an hour from one address, and sign-in to
# thirty a quarter hour. The server judges the connection's own address, and
# every 127.x.y.z is this machine, so each batch of four people registers from
# the next loopback address: the limit is met, not lifted.
PEOPLE_MADE=0
from_address() { echo "127.0.1.$((10 + PEOPLE_MADE / 4))"; }

# sign_in USERNAME — prints a bearer token.
sign_in() {
  curl -sf --interface "$(from_address)" -X POST "${API}/api/auth/signin" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"${PASSWORD}\"}" | json 'd["token"]'
}

# person USERNAME [DISPLAY NAME] — registers somebody and signs them in,
# recording their id and token in people.json for the browser spec.
PEOPLE="$WORK/people.json"
echo '{}' >"$PEOPLE"
person() {
  local id token
  id="$(curl -sf --interface "$(from_address)" -X POST "${API}/api/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"${PASSWORD}\",\"displayName\":\"${2:-}\"}" \
    | json 'd["id"]')" || fail "registering $1"
  token="$(sign_in "$1")" || fail "signing $1 in"
  PEOPLE_MADE=$((PEOPLE_MADE + 1))
  python3 - "$PEOPLE" "$1" "$id" "$token" <<'PY2'
import json, sys
path, name, id_, token = sys.argv[1:]
people = json.load(open(path))
people[name] = {"id": id_, "token": token}
json.dump(people, open(path, "w"))
PY2
}
token_of() { python3 -c "import json,sys; print(json.load(open('$PEOPLE'))['$1']['token'])"; }
id_of() { python3 -c "import json,sys; print(json.load(open('$PEOPLE'))['$1']['id'])"; }

# api TOKEN METHOD PATH [BODY] — prints the body; fails on an error status.
api() {
  local token="$1" method="$2" path="$3" body="${4:-}"
  curl -sf -X "$method" "${API}${path}" -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' ${body:+-d "$body"}
}

# status TOKEN METHOD PATH [BODY] — prints only the HTTP status.
status() {
  local token="$1" method="$2" path="$3" body="${4:-}"
  curl -s -o /dev/null -w '%{http_code}' -X "$method" "${API}${path}" \
    -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' ${body:+-d "$body"}
}

# setting NAME VALUE — as the founder, from the administration settings.
setting() { api "$FOUNDER" PUT "/api/admin/settings/$1" "{\"value\":\"$2\"}" >/dev/null; }
# feature NAME true|false
feature() { api "$FOUNDER" PUT "/api/admin/features/$1" "{\"enabled\":$2}" >/dev/null; }

# channel SLUG [VISIBILITY] — the founder makes a channel; prints its id.
channel() {
  api "$FOUNDER" POST /api/rooms "{\"slug\":\"$1\",\"name\":\"$1\",\"visibility\":\"${2:-public}\"}" | json 'd["id"]'
}

# browser SPEC [ENV=VALUE...] — drives the real web client through a spec.
browser() {
  local spec="$1"
  shift
  (cd "$ROOT/apps/web" && env TDC_TEST_URL="$BASE" TDC_PEOPLE_FILE="$PEOPLE" TDC_DEMO_NAME="$DEMO_NAME" "$@" \
    bunx playwright test -c playwright.events.config.ts "$spec" --reporter=list) \
    || { tail -30 "$WORK/server.log"; fail "the browser part of this milestone failed"; }
}
