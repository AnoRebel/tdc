#!/usr/bin/env bash
#
# The federation Milestone 6 demonstration.
#
# Three servers. A person browses the servers their own is connected to, then
# browses a peer's rooms through it — seeing that peer's public rooms and not
# its private one. A limited server then tells an unlisted peer nothing at all,
# which is different from refusing it.
#
# Three rather than two because the interesting case is a server that is
# connected to one peer and not another: a list that showed everything would
# pass with two servers and be wrong.
#
# Usage: scripts/demo-milestone-federation-6.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT_A="${TDC_DEMO_PORT_A:-8111}"
PORT_B="${TDC_DEMO_PORT_B:-8112}"
PORT_LIAR="${TDC_DEMO_PORT_LIAR:-8113}"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"

KEEP_WORK="${TDC_DEMO_KEEP:-}"
cleanup() {
  for pid in ${SERVER_PIDS:-}; do kill "$pid" 2>/dev/null || true; done
  [[ -n "$KEEP_WORK" ]] && { echo "logs kept in $WORK" >&2; return; }
  rm -rf "$WORK"
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }

say "Building the server"
(cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)

# Each server gets its own database: two servers sharing one would share a
# keyring, and "two servers verified each other" would prove nothing.
start_server() {
  local name="$1" port="$2" canonical="$3"
  local database="tdc_fed_${name}"

  docker exec tdc-database-1 psql -U tdc -d postgres \
    -c "DROP DATABASE IF EXISTS ${database};" >/dev/null
  docker exec tdc-database-1 psql -U tdc -d postgres \
    -c "CREATE DATABASE ${database};" >/dev/null

  TDC_DATABASE_URL="postgres://tdc:tdc@127.0.0.1:55433/${database}?sslmode=disable" \
  TDC_SERVER_ADDRESS="localhost:${port}" \
  TDC_SERVER_CANONICAL_ADDRESS="${canonical}" \
  TDC_SERVER_SECURE_COOKIES=false \
    "$WORK/tdc-server" >"$WORK/${name}.log" 2>&1 &
  SERVER_PIDS="${SERVER_PIDS:-} $!"

  for _ in $(seq 1 30); do
    curl -sf "http://localhost:${port}/api/server" >/dev/null 2>&1 && return 0
    sleep 1
  done
  echo "server ${name} did not start; see $WORK/${name}.log" >&2
  return 1
}

enable_federation() {
  local port="$1"
  local base="http://localhost:${port}"

  curl -sf -X POST "${base}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null

  local token
  token="$(curl -sf -X POST "${base}/api/auth/signin" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" \
    -c "$WORK/cookies-${port}" -o /dev/null -w '%{http_code}')"
  [[ "$token" == "200" ]] || { echo "sign-in to ${base} failed (${token})" >&2; return 1; }

  curl -sf -X PUT "${base}/api/admin/settings/federation.enabled" \
    -H 'Content-Type: application/json' -b "$WORK/cookies-${port}" \
    -d '{"value":"true"}' >/dev/null

  # Two servers on loopback are, by every definition the dialler uses, on a
  # private address. An operator federating inside one network opts into that
  # deliberately; this demonstration is that operator.
  curl -sf -X PUT "${base}/api/admin/settings/federation.allow_private_addresses" \
    -H 'Content-Type: application/json' -b "$WORK/cookies-${port}" \
    -d '{"value":"true"}' >/dev/null
}

say "Three servers: A and B federated, C connected to A only"
start_server a "$PORT_A" "http://a.localhost:${PORT_A}"
start_server b "$PORT_B" "http://b.localhost:${PORT_B}"
start_server c "$PORT_LIAR" "http://c.localhost:${PORT_LIAR}"
enable_federation "$PORT_A"
enable_federation "$PORT_B"
enable_federation "$PORT_LIAR"

sign_in() {
  local port="$1"
  curl -sf -X POST "http://localhost:${port}/api/auth/signin" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" \
    -c "$WORK/session-${port}" -o /dev/null
}

api() {
  local port="$1" method="$2" path="$3" body="${4:-}"
  if [[ -n "$body" ]]; then
    curl -s -X "$method" "http://localhost:${port}${path}" \
      -H 'Content-Type: application/json' -b "$WORK/session-${port}" -d "$body"
  else
    curl -s -X "$method" "http://localhost:${port}${path}" -b "$WORK/session-${port}"
  fi
}

sign_in "$PORT_A"
sign_in "$PORT_B"
sign_in "$PORT_LIAR"
note "A is a.localhost:${PORT_A}"
note "B is b.localhost:${PORT_B}"
note "C is c.localhost:${PORT_LIAR} — connected to A, in no shared room with B"

say "B publishes a public room and a private one"
api "$PORT_B" POST /api/rooms '{"slug":"market","name":"Market","visibility":"public"}' >/dev/null
api "$PORT_B" POST /api/rooms '{"slug":"committee","name":"Committee","visibility":"private"}' >/dev/null
note "market is public, committee is private"

say "A federates a room to B, which is what connects them"
ROOM_A="$(api "$PORT_A" POST /api/rooms '{"slug":"townsquare","name":"Town square","visibility":"public"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
api "$PORT_A" PUT "/api/admin/rooms/${ROOM_A}/federation" \
  "{\"federated\":true,\"peers\":[\"http://b.localhost:${PORT_B}\"]}" >/dev/null

say "And C federates a room to A, which is what connects those two"
ROOM_C="$(api "$PORT_LIAR" POST /api/rooms '{"slug":"harbour","name":"Harbour","visibility":"public"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
api "$PORT_LIAR" PUT "/api/admin/rooms/${ROOM_C}/federation" \
  "{\"federated\":true,\"peers\":[\"http://a.localhost:${PORT_A}\"]}" >/dev/null

say "A person on A sees which servers A is connected to"
api "$PORT_A" GET /api/federation/servers | python3 -m json.tool | sed 's/^/  /'

say "And browses B's rooms, through A"
api "$PORT_A" GET "/api/federation/servers/b.localhost/rooms" | python3 -c '
import json, sys
rooms = json.load(sys.stdin)
if not rooms:
    print("  FAILED: browsing B returned nothing")
    raise SystemExit(1)
for room in rooms:
    print("  %-12s %-10s %s" % (room["slug"], room["visibility"], room["address"]))

slugs = [room["slug"] for room in rooms]
if "committee" in slugs:
    print("  FAILED: B private room was disclosed")
    raise SystemExit(1)
if "market" not in slugs:
    print("  FAILED: B public room was not listed")
    raise SystemExit(1)
print("  market is listed; committee is not")
'

say "C is connected to A but shares no room with B"
api "$PORT_LIAR" GET /api/federation/servers | python3 -c '
import json, sys
servers = [entry["domain"] for entry in json.load(sys.stdin)]
print("  C is connected to:", servers or "(nothing)")
if "b.localhost" in servers:
    print("  FAILED: C lists B, which it shares no room with")
    raise SystemExit(1)
'

say "B makes its private room private at the server level too"
setting() {
  local port="$1" key="$2" value="$3" status
  status="$(curl -s -o /dev/null -w '%{http_code}' \
    -X PUT "http://localhost:${port}/api/admin/settings/${key}" \
    -H 'Content-Type: application/json' -b "$WORK/session-${port}" \
    -d "{\"value\":\"${value}\"}")"
  if [[ "$status" != "204" ]]; then
    echo "FAILED: setting ${key} on ${port} returned ${status}" >&2
    exit 1
  fi
}
setting "$PORT_B" server.visibility limited
setting "$PORT_B" server.visible_to "somebody-else.example.org"

say "Now A, which B has not listed, is told nothing at all"
api "$PORT_A" GET "/api/federation/servers/b.localhost/rooms" | python3 -c '
import json, sys
rooms = json.load(sys.stdin)
if rooms:
    print("  FAILED: a limited server answered a peer it has not listed:", rooms)
    raise SystemExit(1)
print("  nothing — not a refusal, which would itself disclose there is something to refuse")
'

say "And listing A again shows B is still connected, just not forthcoming"
setting "$PORT_B" server.visible_to "a.localhost"
api "$PORT_A" GET "/api/federation/servers/b.localhost/rooms" | python3 -c '
import json, sys
slugs = [room["slug"] for room in json.load(sys.stdin)]
print("  once A is listed again:", slugs)
if "market" not in slugs:
    print("  FAILED: A was listed and still saw nothing")
    raise SystemExit(1)
if "committee" in slugs:
    print("  FAILED: the private room appeared once server visibility allowed it")
    raise SystemExit(1)
'

say "Milestone 6 demonstrated"
note "a person browsed the servers their own server is connected to"
note "browsing a peer listed its public rooms and not its private one"
note "a limited server told an unlisted peer nothing, without saying so"
note "room visibility held even once server visibility allowed the peer"
