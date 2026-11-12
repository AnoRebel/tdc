#!/usr/bin/env bash
#
# The federation Milestone 5 demonstration.
#
# A user on one server joins a room hosted on another and posts into it; the
# second server is then stopped entirely, messages accumulate, and it catches
# up in one request with nothing duplicated.
#
# The server is stopped with SIGSTOP rather than killed, so it is unreachable
# without losing anything — which is what an outage actually looks like, and
# what makes the catch-up afterwards meaningful.
#
# Usage: scripts/demo-milestone-federation-5.sh
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

timeline() {
  api "$1" GET "/api/rooms/$2/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
for item in items:
    content = item.get("content") or {}
    if isinstance(content, str):
        content = json.loads(content)
    body = content.get("body") or ("(%s)" % item.get("type", "event"))
    print("  [seq %s] %-24s %s" % (item.get("sequence"), item.get("sender") or item.get("senderAddress") or "?", body))
'
}

sign_in "$PORT_A"
sign_in "$PORT_B"
note "server A is a.localhost:${PORT_A} — it owns the room"
note "server B is b.localhost:${PORT_B} — its user will join"

say "A creates a room and federates it"
ROOM_A="$(api "$PORT_A" POST /api/rooms '{"slug":"townsquare","name":"Town square","visibility":"public"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
api "$PORT_A" PUT "/api/admin/rooms/${ROOM_A}/federation" \
  "{\"federated\":true,\"peers\":[\"http://b.localhost:${PORT_B}\"]}" >/dev/null

say "B adopts it, naming A as its home server"
ROOM_B="$(api "$PORT_B" POST /api/admin/federation/adopt \
  "{\"slug\":\"townsquare\",\"name\":\"Town square\",\"homeServer\":\"a.localhost\",\"peer\":\"http://a.localhost:${PORT_A}\"}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"

say "B's user joins the room, which A decides"
RESULT="$(api "$PORT_B" POST "/api/rooms/${ROOM_B}/remote/join")"
echo "  $RESULT"
if ! echo "$RESULT" | grep -q '"joined":true'; then
  echo "FAILED: the remote join was refused" >&2
  exit 1
fi

say "B's user posts, and A sequences it"
RESULT="$(api "$PORT_B" POST "/api/rooms/${ROOM_B}/remote/messages" \
  '{"body":"Hello from the other server."}')"
echo "  $RESULT"
if ! echo "$RESULT" | grep -q '"sequence"'; then
  echo "FAILED: the remote post was refused" >&2
  exit 1
fi

say "A posts too, and delivers everything to B"
api "$PORT_A" POST "/api/rooms/${ROOM_A}/messages" '{"body":"Welcome, neighbour."}' >/dev/null
api "$PORT_A" POST "/api/admin/rooms/${ROOM_A}/federation/deliver" >/dev/null

say "What A holds — the room it owns"
timeline "$PORT_A" "$ROOM_A"

say "What B holds — its replica, with its own user attributed to b.localhost"
timeline "$PORT_B" "$ROOM_B"

say "Now B goes away entirely"
kill -STOP "$(pgrep -u "$(id -u)" -f "tdc-server" | while read -r pid; do
  if grep -qa "${PORT_B}" "/proc/${pid}/environ" 2>/dev/null; then echo "$pid"; fi
done | head -1)" 2>/dev/null || true
note "B is stopped — it answers nothing"

say "A posts several messages while B cannot hear them"
for line in "The bridge is closed today." "Fish arrives at eleven." "Bring a lamp tonight."; do
  api "$PORT_A" POST "/api/rooms/${ROOM_A}/messages" "{\"body\":\"${line}\"}" >/dev/null
  echo "  A: ${line}"
done

say "Delivery to B fails, is counted, and is scheduled to retry"
api "$PORT_A" POST "/api/admin/rooms/${ROOM_A}/federation/deliver" | python3 -m json.tool | sed 's/^/  /'

say "B comes back"
kill -CONT "$(pgrep -u "$(id -u)" -f "tdc-server" | while read -r pid; do
  if grep -qa "${PORT_B}" "/proc/${pid}/environ" 2>/dev/null; then echo "$pid"; fi
done | head -1)" 2>/dev/null || true
sleep 2
note "B is running again"

say "B asks A for everything it missed, in one request"
api "$PORT_B" POST "/api/admin/rooms/${ROOM_B}/federation/catchup" | python3 -m json.tool | sed 's/^/  /'

say "And asks again, to show nothing is duplicated"
api "$PORT_B" POST "/api/admin/rooms/${ROOM_B}/federation/catchup" | python3 -m json.tool | sed 's/^/  /'

say "What B holds now"
timeline "$PORT_B" "$ROOM_B"

export PORT_A PORT_B ROOM_A ROOM_B
say "And the client is told where the room lives"
# The room response carries homeServer only for a replica, which is what the
# web client branches on: join and post go through a different path, and the
# header says which server actually holds the room.
api "$PORT_B" GET "/api/rooms/${ROOM_B}" | python3 -c '
import json, sys
room = json.load(sys.stdin)
home = room.get("homeServer")
if not home:
    print("  FAILED: B does not report that the room is hosted elsewhere")
    raise SystemExit(1)
print("  B reports homeServer=%s — the client knows to use the remote path" % home)
'
api "$PORT_A" GET "/api/rooms/${ROOM_A}" | python3 -c '
import json, sys
room = json.load(sys.stdin)
if room.get("homeServer"):
    print("  FAILED: A reports a home server for a room it owns")
    raise SystemExit(1)
print("  A reports no homeServer for its own room — unchanged for ordinary rooms")
'

say "The two servers agree, message for message"
python3 - "$WORK" <<'PYEOF'
import json, subprocess, sys

work = sys.argv[1]

def sequences(port, room):
    out = subprocess.run(
        ["curl", "-s", f"http://localhost:{port}/api/rooms/{room}/timeline",
         "-b", f"{work}/session-{port}"],
        capture_output=True, text=True, check=True).stdout
    payload = json.loads(out)
    items = payload if isinstance(payload, list) else payload.get("items", [])
    return [(item.get("sequence"), item.get("id")) for item in items]

import os
a = sequences(os.environ["PORT_A"], os.environ["ROOM_A"])
b = sequences(os.environ["PORT_B"], os.environ["ROOM_B"])

# Each server has its own sequence 1: A's is the room's creation, B's is the
# local join that made the replica visible to its own user. Everything A
# actually replicated starts at 2, and that is what must match.
shared_a = [entry for entry in a if entry[0] > 1]
shared_b = [entry for entry in b if entry[0] > 1]

if shared_a != shared_b:
    print("  FAILED: the servers disagree")
    print("   A:", shared_a)
    print("   B:", shared_b)
    raise SystemExit(1)

ids = [event for _, event in b]
if len(ids) != len(set(ids)):
    print("  FAILED: B holds duplicates")
    raise SystemExit(1)

print("  %d replicated events, identical sequences and identifiers on both servers" % len(shared_a))
PYEOF

say "Milestone 5 demonstrated"
note "a user on B joined a room on A and posted into it"
note "the message is attributed to its own server, and A assigned its position"
note "B missed messages while stopped and caught up in one request"
note "asking again added nothing: redelivery is a no-op"
