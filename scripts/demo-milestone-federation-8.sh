#!/usr/bin/env bash
#
# The federation Milestone 8 demonstration.
#
# Two servers federated using only what the published documentation says, with
# each step traceable to a section of it rather than to the source: the
# well-known document and version negotiation from the protocol document, the
# federation settings from the hosting guide, and the homeServer field and
# remote-post endpoint from the integration guide.
#
# If a step here needs something the documents do not state, the documents are
# wrong — which is the failure this exists to catch.
#
# Usage: scripts/demo-milestone-federation-8.sh
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

say "Two servers, set up following the hosting guide alone"
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

setting() {
  local port="$1" key="$2" value="$3" status
  status="$(curl -s -o /dev/null -w '%{http_code}' \
    -X PUT "http://localhost:${port}/api/admin/settings/${key}" \
    -H 'Content-Type: application/json' -b "$WORK/session-${port}" \
    -d "{\"value\":\"${value}\"}")"
  [[ "$status" == "204" ]] || { echo "FAILED: setting ${key} returned ${status}" >&2; exit 1; }
}

sign_in "$PORT_A"
sign_in "$PORT_B"

say "Hosting guide: 'Talking to other servers' — each publishes its identity"
# The document says a server publishes its identity at a well-known path, and
# that the canonical address must match the domain serving it.
for port in "$PORT_A" "$PORT_B"; do
  curl -sf "http://localhost:${port}/.well-known/tdc-federation" \
    | python3 -c '
import json, sys
doc = json.load(sys.stdin)
for field in ("canonicalAddress", "keys", "versions", "enabled"):
    if field not in doc:
        print("  FAILED: the document lacks %s, which the protocol requires" % field)
        raise SystemExit(1)
print("  %s speaks %s, publishing %d key(s)" % (doc["canonicalAddress"], ",".join(doc["versions"]), len(doc["keys"])))
'
done

say "Protocol document: 'Agreeing on a version' — both speak tdc/1"
api "$PORT_A" POST /api/federation/version '{"versions":["tdc/1"]}' | sed 's/^/  /'

say "And two servers sharing no version refuse, naming both lists"
RESULT="$(api "$PORT_A" POST /api/federation/version '{"versions":["tdc/99"]}')"
echo "  $RESULT"
echo "$RESULT" | grep -q "tdc/99" && echo "$RESULT" | grep -q "tdc/1" \
  || { echo "FAILED: the refusal does not name both version lists" >&2; exit 1; }

say "Hosting guide: federate a room, and adopt it on the other side"
ROOM_A="$(api "$PORT_A" POST /api/rooms '{"slug":"townsquare","name":"Town square","visibility":"public"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
api "$PORT_A" PUT "/api/admin/rooms/${ROOM_A}/federation" \
  "{\"federated\":true,\"peers\":[\"http://b.localhost:${PORT_B}\"]}" >/dev/null
ROOM_B="$(api "$PORT_B" POST /api/admin/federation/adopt \
  "{\"slug\":\"townsquare\",\"name\":\"Town square\",\"homeServer\":\"a.localhost\",\"peer\":\"http://a.localhost:${PORT_A}\"}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"

say "Integration guide: a room another server owns reports its homeServer"
api "$PORT_B" GET "/api/rooms/${ROOM_B}" | python3 -c '
import json, sys
room = json.load(sys.stdin)
if room.get("homeServer") != "a.localhost":
    print("  FAILED: homeServer is %r, which the integration guide says to branch on" % room.get("homeServer"))
    raise SystemExit(1)
print("  homeServer=%s — a program branches here, as documented" % room["homeServer"])
'
api "$PORT_A" GET "/api/rooms/${ROOM_A}" | python3 -c '
import json, sys
if json.load(sys.stdin).get("homeServer"):
    print("  FAILED: an ordinary room reports a homeServer")
    raise SystemExit(1)
print("  an ordinary room omits it entirely, as documented")
'

say "Messages from both sides"
api "$PORT_A" POST "/api/rooms/${ROOM_A}/messages" '{"body":"From the server that owns the room."}' >/dev/null
api "$PORT_B" POST "/api/rooms/${ROOM_B}/remote/join" >/dev/null
# The integration guide's documented endpoint for a room another server owns.
POSTED="$(api "$PORT_B" POST "/api/rooms/${ROOM_B}/remote/messages" '{"body":"From the server that replicates it."}')"
echo "  B posted: $POSTED"
echo "$POSTED" | grep -q '"sequence"' \
  || { echo "FAILED: the documented remote-post response has no sequence" >&2; exit 1; }

api "$PORT_A" POST "/api/admin/rooms/${ROOM_A}/federation/deliver" >/dev/null
api "$PORT_B" POST "/api/admin/rooms/${ROOM_B}/federation/catchup" >/dev/null

say "Both servers hold both messages"
for pair in "$PORT_A:$ROOM_A" "$PORT_B:$ROOM_B"; do
  port="${pair%%:*}"; room="${pair#*:}"
  api "$port" GET "/api/rooms/${room}/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
bodies = []
for item in items:
    content = item.get("content") or {}
    if isinstance(content, str):
        content = json.loads(content)
    if content.get("body"):
        bodies.append((item.get("sender"), content["body"]))
for sender, body in bodies:
    print("  %-24s %s" % (sender, body))
if len(bodies) < 2:
    print("  FAILED: only %d message(s) — both sides should be present" % len(bodies))
    raise SystemExit(1)
'
done

say "Hosting guide: 'Choosing who to federate with' — B blocks A"
setting "$PORT_B" federation.blocked_servers "a.localhost"

say "And the block takes effect at once, in both directions"
OUT="$(api "$PORT_B" POST /api/admin/federation/ping "{\"peer\":\"http://a.localhost:${PORT_A}\"}")"
echo "  B to A: $OUT"
echo "$OUT" | grep -q "blocked" || { echo "FAILED: B still reaches a server it blocked" >&2; exit 1; }

IN="$(api "$PORT_A" POST /api/admin/federation/ping "{\"peer\":\"http://b.localhost:${PORT_B}\"}")"
echo "  A to B: $IN"
echo "$IN" | grep -q '"reached":false' || { echo "FAILED: B accepted a request from a server it blocked" >&2; exit 1; }

say "Milestone 8 demonstrated"
note "two servers deployed and federated from the documentation alone"
note "identity, version negotiation, and the refusal each match the protocol document"
note "messages crossed in both directions, using the documented endpoints"
note "a block took effect immediately, in both directions"
