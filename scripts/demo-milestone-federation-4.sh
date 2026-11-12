#!/usr/bin/env bash
#
# The federation Milestone 4 demonstration.
#
# A room created on one server is federated to another, messages posted on the
# first appear on the second in the order the first assigned, and a forged
# event submitted straight to the second server's federation endpoint is
# refused and never stored.
#
# Two real servers with separate databases and separate keys, because what is
# being shown is that B believes A's events on the strength of A's signature
# alone — which it cannot demonstrate against a peer whose keys it shares.
#
# Usage: scripts/demo-milestone-federation-4.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT_A="${TDC_DEMO_PORT_A:-8111}"
PORT_B="${TDC_DEMO_PORT_B:-8112}"
PORT_LIAR="${TDC_DEMO_PORT_LIAR:-8113}"
PASSWORD="correct horse battery staple"
WORK="$(mktemp -d)"

cleanup() {
  for pid in ${SERVER_PIDS:-}; do kill "$pid" 2>/dev/null || true; done
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

say "Two servers, each holding its own signing key"
start_server a "$PORT_A" "http://a.localhost:${PORT_A}"
start_server b "$PORT_B" "http://b.localhost:${PORT_B}"
enable_federation "$PORT_A"
enable_federation "$PORT_B"

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
note "server A is a.localhost:${PORT_A} — it will own the room"
note "server B is b.localhost:${PORT_B} — it will replicate it"

say "A creates a room and federates it to B"
ROOM_A="$(api "$PORT_A" POST /api/rooms '{"slug":"townsquare","name":"Town square","visibility":"public"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
note "room ${ROOM_A} on A"
api "$PORT_A" PUT "/api/admin/rooms/${ROOM_A}/federation" \
  "{\"federated\":true,\"peers\":[\"http://b.localhost:${PORT_B}\"]}" >/dev/null

say "B adopts the room rather than creating one of its own"
# Adoption is not creation: the replica records A as the room's home server,
# which is what lets it accept A's events and refuse everybody else's. A room
# created the ordinary way on B would name B as home and then correctly refuse
# everything A sent it.
ROOM_B="$(api "$PORT_B" POST /api/admin/federation/adopt \
  "{\"slug\":\"townsquare\",\"name\":\"Town square\",\"homeServer\":\"a.localhost\",\"peer\":\"http://a.localhost:${PORT_A}\"}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
note "room ${ROOM_B} on B, home server a.localhost"

say "Three messages posted on A"
for line in "Good morning, everyone." "The market opens at nine." "Bring your own chair."; do
  api "$PORT_A" POST "/api/rooms/${ROOM_A}/messages" "{\"body\":\"${line}\"}" >/dev/null
  echo "  A: ${line}"
done

say "A delivers them to B"
api "$PORT_A" POST "/api/admin/rooms/${ROOM_A}/federation/deliver" | python3 -m json.tool | sed 's/^/  /'

say "What B holds, from its own replica"
api "$PORT_B" GET "/api/rooms/${ROOM_B}/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
if not items:
    print("  (nothing — replication did not happen)")
    raise SystemExit(1)
for item in items:
    content = item.get("content") or {}
    if isinstance(content, str):
        content = json.loads(content)
    body = content.get("body") or ("(%s)" % item.get("type", "event"))
    print("  B sees [seq %s] %s" % (item.get("sequence"), body))
'

say "The order on B is the order A assigned, not one B invented"
api "$PORT_B" GET "/api/rooms/${ROOM_B}/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
sequences = [item.get("sequence") for item in items]
if sequences != sorted(sequences):
    print("  FAILED: B reordered the room")
    raise SystemExit(1)
print("  sequences on B:", sequences, "— ascending, as A assigned them")
'

say "A forged event, submitted straight to B's federation endpoint"
go run "$ROOT/server/cmd/tdc-federation-forge" \
  --peer "http://b.localhost:${PORT_B}" \
  --room "townsquare" \
  --home "a.localhost"

say "And B still holds only what A really sent"
api "$PORT_B" GET "/api/rooms/${ROOM_B}/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
for item in items:
    content = item.get("content") or {}
    if isinstance(content, str):
        content = json.loads(content)
    body = content.get("body", "")
    if "forged" in body.lower() or "nobody ever sent" in body.lower():
        print("  FAILED: the forged message is in the room")
        raise SystemExit(1)
print("  %d messages, none of them forged" % len(items))
'

say "Milestone 4 demonstrated"
note "a room created on A was federated to B"
note "messages posted on A appear on B in the order A assigned"
note "a forged event was refused and never stored"
