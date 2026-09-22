#!/usr/bin/env bash
#
# The federation Milestone 7 demonstration.
#
# A moderator on the server holding a replica redacts a message that
# originated elsewhere. It goes from their server and stays on the origin's,
# and no request is sent asking the origin to agree.
#
# That asymmetry is the point rather than a shortcoming: a server cannot compel
# another to forget something, and a protocol that pretended otherwise would
# tell a moderator their decision travelled when it did not.
#
# Usage: scripts/demo-milestone-federation-7.sh
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
    print("  [seq %s] %-24s %s" % (item.get("sequence"), item.get("sender") or "?", body))
'
}

sign_in "$PORT_A"
sign_in "$PORT_B"
note "A is a.localhost:${PORT_A} — it owns the room"
note "B is b.localhost:${PORT_B} — it holds a replica and has its own moderator"

say "A creates a room, federates it, and posts"
ROOM_A="$(api "$PORT_A" POST /api/rooms '{"slug":"townsquare","name":"Town square","visibility":"public"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
api "$PORT_A" PUT "/api/admin/rooms/${ROOM_A}/federation" \
  "{\"federated\":true,\"peers\":[\"http://b.localhost:${PORT_B}\"]}" >/dev/null

ROOM_B="$(api "$PORT_B" POST /api/admin/federation/adopt \
  "{\"slug\":\"townsquare\",\"name\":\"Town square\",\"homeServer\":\"a.localhost\",\"peer\":\"http://a.localhost:${PORT_A}\"}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"

for line in "Good morning." "Something B's moderator will object to." "Carry on."; do
  api "$PORT_A" POST "/api/rooms/${ROOM_A}/messages" "{\"body\":\"${line}\"}" >/dev/null
done
DELIVERY="$(api "$PORT_A" POST "/api/admin/rooms/${ROOM_A}/federation/deliver")"
if ! echo "$DELIVERY" | grep -q '"stored"'; then
  echo "FAILED: delivery to B did not store anything: $DELIVERY" >&2
  exit 1
fi

say "Both servers hold the same three messages"
echo "  --- on A"; timeline "$PORT_A" "$ROOM_A"
echo "  --- on B"; timeline "$PORT_B" "$ROOM_B"

say "B's moderator redacts the middle one, which originated on A"
TARGET="$(api "$PORT_B" GET "/api/rooms/${ROOM_B}/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
for item in items:
    content = item.get("content") or {}
    if isinstance(content, str):
        content = json.loads(content)
    if "object to" in (content.get("body") or ""):
        print(item["id"]); break
')"
note "redacting ${TARGET}"

# B has no member row in a replica until somebody joins, and moderation needs
# membership — which is the ordinary path a person takes.
api "$PORT_B" POST "/api/rooms/${ROOM_B}/join" >/dev/null
RESULT="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE \
  "http://localhost:${PORT_B}/api/rooms/${ROOM_B}/messages/$(python3 -c "
import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=''))" "$TARGET")" \
  -b "$WORK/session-${PORT_B}")"
if [[ "$RESULT" != "204" && "$RESULT" != "201" ]]; then
  echo "FAILED: B's redaction returned ${RESULT}" >&2
  exit 1
fi
note "B's redaction returned ${RESULT}"

say "Gone from B"
timeline "$PORT_B" "$ROOM_B"
api "$PORT_B" GET "/api/rooms/${ROOM_B}/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
if not items:
    print("  FAILED: B holds nothing at all — replication did not happen")
    raise SystemExit(1)
bodies = []
for item in items:
    content = item.get("content") or {}
    if isinstance(content, str):
        content = json.loads(content)
    bodies.append(content.get("body") or "")
if any("object to" in body for body in bodies):
    print("  FAILED: the redacted message is still readable on B")
    raise SystemExit(1)
if not any("Good morning" in body for body in bodies):
    print("  FAILED: B lost more than the redacted message")
    raise SystemExit(1)
print("  the redacted message is gone; its neighbours are untouched")
'

say "Still intact on A, which was never asked"
timeline "$PORT_A" "$ROOM_A"
api "$PORT_A" GET "/api/rooms/${ROOM_A}/timeline" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload if isinstance(payload, list) else payload.get("items", [])
for item in items:
    content = item.get("content") or {}
    if isinstance(content, str):
        content = json.loads(content)
    if "object to" in (content.get("body") or ""):
        print("  A still holds it in full, as it must")
        raise SystemExit(0)
print("  FAILED: A lost the message too, which no other server may cause")
raise SystemExit(1)
'

say "No request reached A for it"
if grep -qE "redact|delete" "$WORK/a.log"; then
  echo "  FAILED: A's log mentions a redaction request" >&2
  exit 1
fi
note "A's log contains no redaction request — the decision was B's alone"

say "And B recorded it"
api "$PORT_B" GET "/api/admin/audit?limit=20" | python3 -c '
import json, sys
entries = json.load(sys.stdin)
shown = [e for e in entries if "message" in e["action"] or "redact" in e["action"] or "delete" in e["action"]]
for entry in shown[:3]:
    print("  %-32s %s" % (entry["action"], entry.get("targetId", "")))
if not shown:
    print("  (no moderation entry — see note below)")
'

say "Milestone 7 demonstrated"
note "a moderator on B redacted a message that originated on A"
note "it is gone from B and untouched on A"
note "no request was sent to A: the decision is local and unilateral"
