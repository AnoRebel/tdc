#!/usr/bin/env bash
#
# Project federation Milestone 4: changes from anywhere.
#
# Two servers built from this checkout. A is home to a private project with a
# member on B. That member files an issue from B; a member on A moves it; she
# comments from B and is assigned it on A — and both servers show the same issue,
# in the same column, with the same history. A deletion her role does not allow
# is refused by A, in A's words, on B.
#
# Usage: scripts/demo-milestone-project-federation-4.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT_A="${TDC_DEMO_PORT_A:-8431}"
PORT_B="${TDC_DEMO_PORT_B:-8432}"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"

cleanup() {
  for pid in ${SERVER_PIDS:-}; do kill "$pid" 2>/dev/null || true; done
  if [[ -z "${KEEP_DEMO:-}" ]]; then
    for name in a b; do
      docker exec tdc-database-1 psql -U tdc -d postgres -q \
        -c "DROP DATABASE IF EXISTS tdc_pf4_${name} WITH (FORCE);" >/dev/null 2>&1 || true
    done
    rm -rf "$WORK"
  else
    echo "kept: logs in $WORK"
  fi
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
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
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

# eventually SECONDS COMMAND... — the command succeeds within the time, polled.
eventually() {
  local limit="$1"
  shift
  for _ in $(seq 1 "$limit"); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}

say "Building the server"
(cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)

start_server() {
  local name="$1" port="$2"
  local database="tdc_pf4_${name}"
  docker exec tdc-database-1 psql -U tdc -d postgres -q \
    -c "DROP DATABASE IF EXISTS ${database} WITH (FORCE);" >/dev/null
  docker exec tdc-database-1 psql -U tdc -d postgres -q -c "CREATE DATABASE ${database};" >/dev/null
  TDC_DATABASE_URL="postgres://tdc:tdc@127.0.0.1:55433/${database}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${port}" \
  TDC_SERVER_CANONICAL_ADDRESS="http://${name}.localhost:${port}" \
  TDC_SERVER_SECURE_COOKIES=false \
    "$WORK/tdc-server" >"$WORK/${name}.log" 2>&1 &
  SERVER_PIDS="${SERVER_PIDS:-} $!"
  for _ in $(seq 1 300); do
    curl -sf "http://localhost:${port}/api/health" >/dev/null 2>&1 && break
    sleep 1
  done
  curl -sf "http://localhost:${port}/api/health" >/dev/null || { tail -30 "$WORK/${name}.log"; exit 1; }
  curl -sf -X POST "http://localhost:${port}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null
}

# token PORT USERNAME
token() {
  curl -sf -X POST "http://localhost:$1/api/auth/signin" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$2\",\"password\":\"${PASSWORD}\"}" | json 'd["token"]'
}
register() {
  curl -sf -X POST "http://localhost:$1/api/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$2\",\"password\":\"${PASSWORD}\"}" >/dev/null
}
# api PORT TOKEN METHOD PATH [BODY]
api() {
  local port="$1" token="$2" method="$3" path="$4" body="${5:-}"
  curl -sf -X "$method" "http://localhost:${port}${path}" -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' ${body:+-d "$body"}
}
setting() { api "$1" "$2" PUT "/api/admin/settings/$3" "{\"value\":\"$4\"}" >/dev/null; }

say "Two servers, each with its own database and signing key"
start_server a "$PORT_A"
start_server b "$PORT_B"
A="$(token "$PORT_A" founder)"
B="$(token "$PORT_B" founder)"
for port_token in "$PORT_A:$A" "$PORT_B:$B"; do
  setting "${port_token%%:*}" "${port_token#*:}" federation.enabled true
  setting "${port_token%%:*}" "${port_token#*:}" federation.allow_private_addresses true
done
setting "$PORT_A" "$A" federation.mode allowlist
setting "$PORT_A" "$A" federation.allowed_servers "http://b.localhost:${PORT_B}"
register "$PORT_B" dalila
DALILA="$(token "$PORT_B" dalila)"

say "On A: a private project, opened, with dalila@b.localhost as a member"
api "$PORT_A" "$A" POST /api/projects '{"key":"WEB","name":"Website","visibility":"private"}' >/dev/null
api "$PORT_A" "$A" PUT /api/projects/WEB/federation '{"federated":true}' >/dev/null
api "$PORT_A" "$A" PUT /api/projects/WEB/members/dalila@b.localhost '{"role":"member"}' >/dev/null
has_project() { api "$1" "$2" GET /api/projects | json "'$3' in [p['key'] for p in d]" | grep -qx True; }
check "dalila on B has WEB@a.localhost" eventually 30 has_project "$PORT_B" "$DALILA" "WEB@a.localhost"
check "B offers dalila what her role allows: filing, and not deleting" \
  test "$(api "$PORT_B" "$DALILA" GET /api/projects/WEB@a.localhost | json 'd["can"]["createIssues"] and not d["can"]["delete"]')" = True

say "dalila files an issue from B"
FILED="$(api "$PORT_B" "$DALILA" POST /api/projects/WEB@a.localhost/issues '{"title":"Filed from B"}')"
check "B answers with the issue at once, as WEB@a.localhost-1" \
  test "$(echo "$FILED" | json 'd["key"]')" = "WEB@a.localhost-1"
check "A holds WEB-1, written by dalila@b.localhost" \
  test "$(api "$PORT_A" "$A" GET /api/issues/WEB-1 | json 'd["title"]+" by "+d.get("author","")')" = "Filed from B by dalila@b.localhost"
ISSUE="$(echo "$FILED" | json 'd["id"]')"
check "A's history credits dalila with filing it" \
  bash -c "curl -sf -H 'Authorization: Bearer ${A}' http://localhost:${PORT_A}/api/issues/WEB-1/history | grep -q 'dalila@b.localhost'"

say "A moves it; B sees it moved"
PROGRESS="$(api "$PORT_A" "$A" GET /api/projects/WEB | json '[s["id"] for s in d["states"] if s["name"]=="In progress"][0]')"
api "$PORT_A" "$A" POST /api/issues/WEB-1/state "{\"stateId\":\"${PROGRESS}\"}" >/dev/null
state_on() { api "$1" "$2" GET "$3" | json 'd["state"]["name"]'; }
check "WEB-1 is In progress on B" \
  eventually 30 bash -c "[[ \"\$(curl -sf -H 'Authorization: Bearer ${DALILA}' 'http://localhost:${PORT_B}/api/issues/WEB@a.localhost-1' | python3 -c 'import json,sys; print(json.load(sys.stdin)[\"state\"][\"name\"])')\" == 'In progress' ]]"

say "dalila comments from B, and A assigns it to her"
api "$PORT_B" "$DALILA" POST "/api/rooms/${ISSUE}/remote/messages" '{"body":"I can take this one."}' >/dev/null
said() { api "$1" "$2" GET "/api/rooms/$3/timeline" | grep -q "$4"; }
check "her comment is in WEB-1's discussion on A" eventually 30 said "$PORT_A" "$A" "$ISSUE" "I can take this one."
check "and back on B, where she wrote it" eventually 30 said "$PORT_B" "$DALILA" "$ISSUE" "I can take this one."
api "$PORT_A" "$A" PUT /api/issues/WEB-1/assignees/dalila@b.localhost >/dev/null
assigned() { api "$PORT_B" "$DALILA" GET /api/issues/WEB@a.localhost-1 | grep -q 'dalila'; }
check "WEB-1 is assigned to dalila on B" eventually 30 assigned
told() { api "$PORT_B" "$DALILA" GET /api/me/notifications | grep -q 'assigned'; }
check "B told dalila it is hers" eventually 30 told

say "The same issue, in the same column, with the same history, on both servers"
column_of() { api "$1" "$2" GET "/api/projects/$3/board" | json '[col["name"] for col in d["columns"] for c in col["cards"] if c["title"]=="Filed from B"][0]'; }
check "both boards have it In progress" \
  test "$(column_of "$PORT_A" "$A" WEB)/$(column_of "$PORT_B" "$DALILA" WEB@a.localhost)" = "In progress/In progress"
history_of() { api "$1" "$2" GET "/api/issues/$3/history" | json '",".join(e["type"] for e in d)'; }
check "both histories hold the same events, in the same order" \
  eventually 30 bash -c "[[ \"\$(curl -sf -H 'Authorization: Bearer ${A}' http://localhost:${PORT_A}/api/issues/WEB-1/history | python3 -c 'import json,sys; print(\",\".join(e[\"id\"] for e in json.load(sys.stdin)))')\" == \"\$(curl -sf -H 'Authorization: Bearer ${DALILA}' 'http://localhost:${PORT_B}/api/issues/WEB@a.localhost-1/history' | python3 -c 'import json,sys; print(\",\".join(e[\"id\"] for e in json.load(sys.stdin)))')\" ]]"
note "history: $(history_of "$PORT_B" "$DALILA" "WEB@a.localhost-1")"

say "What her role does not allow is refused by A, in A's words"
REFUSED="$(curl -s -X DELETE -H "Authorization: Bearer ${DALILA}" "http://localhost:${PORT_B}/api/issues/WEB@a.localhost-1")"
check "deleting WEB-1 from B is refused, naming issue.delete" bash -c "grep -q 'issue.delete' <<<'${REFUSED}'"
check "and WEB-1 is still on A" test "$(api "$PORT_A" "$A" GET /api/issues/WEB-1 | json 'd["title"]')" = "Filed from B"

say "Project federation Milestone 4 demonstrated"
note "a member on B filed an issue in A's project, a member on A moved it, she commented from B and was assigned"
note "both servers show it in the same column with the same history"
