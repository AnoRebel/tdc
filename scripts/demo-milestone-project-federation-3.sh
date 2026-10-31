#!/usr/bin/env bash
#
# Project federation Milestone 3: replicas.
#
# Three servers built from this checkout. A is home to two private projects
# opened to federation: WEB, with a member on B, and OPS, with a member on C —
# so C is a peer of A in earnest, and WEB not reaching it is a real refusal
# rather than a server nobody was sending anything. WEB appears on B with its
# board and an issue's discussion, follows along as A changes it, never
# appears on C, and is discarded from B when its member there is removed.
#
# Usage: scripts/demo-milestone-project-federation-3.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT_A="${TDC_DEMO_PORT_A:-8421}"
PORT_B="${TDC_DEMO_PORT_B:-8422}"
PORT_C="${TDC_DEMO_PORT_C:-8423}"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"

cleanup() {
  for pid in ${SERVER_PIDS:-}; do kill "$pid" 2>/dev/null || true; done
  if [[ -z "${KEEP_DEMO:-}" ]]; then
    for name in a b c; do
      docker exec tdc-database-1 psql -U tdc -d postgres -q \
        -c "DROP DATABASE IF EXISTS tdc_pf3_${name} WITH (FORCE);" >/dev/null 2>&1 || true
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
  local database="tdc_pf3_${name}"
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

say "Three servers, each with its own database and signing key"
start_server a "$PORT_A"
start_server b "$PORT_B"
start_server c "$PORT_C"
A="$(token "$PORT_A" founder)"
B="$(token "$PORT_B" founder)"
C="$(token "$PORT_C" founder)"
for port_token in "$PORT_A:$A" "$PORT_B:$B" "$PORT_C:$C"; do
  setting "${port_token%%:*}" "${port_token#*:}" federation.enabled true
  # Loopback is a private address; this operator federates inside one machine.
  setting "${port_token%%:*}" "${port_token#*:}" federation.allow_private_addresses true
done
# A federates with B and C by name, and the full addresses say where they are.
setting "$PORT_A" "$A" federation.mode allowlist
setting "$PORT_A" "$A" federation.allowed_servers "http://b.localhost:${PORT_B},http://c.localhost:${PORT_C}"
register "$PORT_B" dalila
register "$PORT_C" chausiku
DALILA="$(token "$PORT_B" dalila)"
CHAUSIKU="$(token "$PORT_C" chausiku)"
note "A is a.localhost:${PORT_A}, B is b.localhost:${PORT_B}, C is c.localhost:${PORT_C}"

say "On A: two private projects, each opened to federation with one member elsewhere"
api "$PORT_A" "$A" POST /api/projects '{"key":"WEB","name":"Website","visibility":"private"}' >/dev/null
api "$PORT_A" "$A" POST /api/projects '{"key":"OPS","name":"Operations","visibility":"private"}' >/dev/null
api "$PORT_A" "$A" POST /api/projects/WEB/issues '{"title":"Ship the replica"}' >/dev/null
api "$PORT_A" "$A" POST /api/projects/OPS/issues '{"title":"Rotate the keys"}' >/dev/null
for key in WEB OPS; do api "$PORT_A" "$A" PUT "/api/projects/${key}/federation" '{"federated":true}' >/dev/null; done
api "$PORT_A" "$A" PUT /api/projects/WEB/members/dalila@b.localhost '{"role":"member"}' >/dev/null
api "$PORT_A" "$A" PUT /api/projects/OPS/members/chausiku@c.localhost '{"role":"member"}' >/dev/null
WEB1="$(api "$PORT_A" "$A" GET /api/issues/WEB-1 | json 'd["id"]')"
api "$PORT_A" "$A" POST "/api/rooms/${WEB1}/messages" '{"body":"Started on this one."}' >/dev/null

say "WEB reaches B, and only B"
has_project() { api "$1" "$2" GET /api/projects | json "'$3' in [p['key'] for p in d]" | grep -qx True; }
check "dalila on B sees WEB@a.localhost among her projects" \
  eventually 30 has_project "$PORT_B" "$DALILA" "WEB@a.localhost"
check "chausiku on C sees OPS@a.localhost, so C is a peer of A in earnest" \
  eventually 30 has_project "$PORT_C" "$CHAUSIKU" "OPS@a.localhost"
board_titles() { api "$1" "$2" GET "/api/projects/$3/board" | json '",".join(sorted(c["title"] for col in d["columns"] for c in col["cards"]))'; }
check "B's board for WEB shows A's issue" \
  test "$(board_titles "$PORT_B" "$DALILA" "WEB@a.localhost")" = "Ship the replica"
check "WEB is nowhere on C, not even to its administrator" \
  bash -c "! curl -sf -H 'Authorization: Bearer ${C}' http://localhost:${PORT_C}/api/projects | grep -q WEB"
check "B and C, holding replicas, offer A's projects to nobody" \
  test "$(docker exec tdc-database-1 psql -U tdc -d tdc_pf3_b -tAc "SELECT count(*) FROM project_replicas")$(docker exec tdc-database-1 psql -U tdc -d tdc_pf3_c -tAc "SELECT count(*) FROM project_replicas")" = 00
check "C's database holds no room of WEB's at all" \
  test "$(docker exec tdc-database-1 psql -U tdc -d tdc_pf3_c -tAc "SELECT count(*) FROM rooms WHERE name = 'Website' OR id = '${WEB1}'")" = 0

say "The issue's discussion is on B, and so is what A says next"
said() { api "$1" "$2" GET "/api/rooms/$3/timeline" | grep -q "$4"; }
check "the comment made on A is in WEB-1's discussion on B" \
  eventually 30 said "$PORT_B" "$DALILA" "$WEB1" "Started on this one."
api "$PORT_A" "$A" POST /api/projects/WEB/issues '{"title":"Document the replica"}' >/dev/null
WEB2="$(api "$PORT_A" "$A" GET /api/issues/WEB-2 | json 'd["id"]')"
api "$PORT_A" "$A" POST "/api/rooms/${WEB2}/messages" '{"body":"From its first message."}' >/dev/null
DONE="$(api "$PORT_A" "$A" GET /api/projects/WEB | json '[s["id"] for s in d["states"] if s["isCompletion"]][0]')"
api "$PORT_A" "$A" POST /api/issues/WEB-1/state "{\"stateId\":\"${DONE}\"}" >/dev/null
check "a new issue filed on A appears on B's board" \
  eventually 30 bash -c "[[ \"\$(curl -sf -H 'Authorization: Bearer ${DALILA}' 'http://localhost:${PORT_B}/api/projects/WEB@a.localhost/board' | python3 -c 'import json,sys; d=json.load(sys.stdin); print(sum(len(c[\"cards\"]) for c in d[\"columns\"]))')\" == 2 ]]"
check "the new issue's discussion is on B from its first message" \
  eventually 30 said "$PORT_B" "$DALILA" "$WEB2" "From its first message."
check "WEB-1 moved to Done on A is Done on B" \
  eventually 30 bash -c "curl -sf -H 'Authorization: Bearer ${DALILA}' 'http://localhost:${PORT_B}/api/issues/WEB@a.localhost-1' | grep -q '\"isCompletion\":true'"
check "B cannot change the replica: filing there is refused" \
  bash -c "[[ \"\$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Authorization: Bearer ${DALILA}' -H 'Content-Type: application/json' -d '{\"title\":\"x\"}' 'http://localhost:${PORT_B}/api/projects/WEB@a.localhost/issues')\" != 201 ]]"

say "Removing B's last member ends B's replica"
api "$PORT_A" "$A" DELETE /api/projects/WEB/members/dalila@b.localhost >/dev/null
gone() { ! has_project "$PORT_B" "$B" "WEB@a.localhost" && test "$(docker exec tdc-database-1 psql -U tdc -d tdc_pf3_b -tAc "SELECT count(*) FROM rooms WHERE id IN ('${WEB1}','${WEB2}')")" = 0; }
check "B no longer holds WEB, its issues or their discussions" eventually 30 gone
check "A no longer delivers WEB to B" \
  test "$(docker exec tdc-database-1 psql -U tdc -d tdc_pf3_a -tAc "SELECT count(*) FROM room_peers WHERE peer = 'b.localhost'")" = 0
check "C still holds OPS" has_project "$PORT_C" "$CHAUSIKU" "OPS@a.localhost"

say "Project federation Milestone 3 demonstrated"
note "a private project on A with a member on B appeared on B with its board and an issue's discussion"
note "it followed A's changes, did not appear on C, and left B when that member was removed"
