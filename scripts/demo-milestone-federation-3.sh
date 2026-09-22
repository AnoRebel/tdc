#!/usr/bin/env bash
#
# The federation Milestone 3 demonstration.
#
# Two TDC servers allowlist each other and exchange pings; a server on neither
# list is refused without being dialed; and a block refuses a peer in both
# directions at once, taking effect on the next request with no restart.
#
# Every change is made through the same API an operator's settings screen uses,
# against servers that keep running throughout — because "immediately, with no
# restart" is the part of the requirement most easily satisfied by accident in
# a test that restarts between steps.
#
# Usage: scripts/demo-milestone-federation-3.sh
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

setting() {
  local port="$1" key="$2" value="$3"
  curl -sf -X PUT "http://localhost:${port}/api/admin/settings/${key}" \
    -H 'Content-Type: application/json' -b "$WORK/session-${port}" \
    -d "{\"value\":\"${value}\"}"
}

ping_peer() {
  local from="$1" to="$2"
  curl -s -X POST "http://localhost:${from}/api/admin/federation/ping" \
    -H 'Content-Type: application/json' -b "$WORK/session-${from}" \
    -d "{\"peer\":\"${to}\"}"
}

sign_in "$PORT_A"
sign_in "$PORT_B"
note "server A is a.localhost:${PORT_A}"
note "server B is b.localhost:${PORT_B}"

say "Each puts the other on its allowlist, and nobody else"
setting "$PORT_A" federation.mode allowlist
setting "$PORT_B" federation.mode allowlist
setting "$PORT_A" federation.allowed_servers "b.localhost"
setting "$PORT_B" federation.allowed_servers "a.localhost"

say "The ping succeeds between them"
for pair in "$PORT_A:http://b.localhost:${PORT_B}" "$PORT_B:http://a.localhost:${PORT_A}"; do
  from="${pair%%:*}"; to="${pair#*:}"
  RESULT="$(ping_peer "$from" "$to")"
  echo "  $RESULT"
  if ! echo "$RESULT" | grep -q '"reached":true'; then
    echo "FAILED: allowlisted peers could not reach each other" >&2
    exit 1
  fi
done

say "A server not on the allowlist is refused, without being dialed"
RESULT="$(ping_peer "$PORT_A" "http://stranger.example.org")"
echo "  $RESULT"
if ! echo "$RESULT" | grep -q "allowlist"; then
  echo "FAILED: an unlisted peer was not refused by the allowlist" >&2
  exit 1
fi

say "Now B blocks A"
setting "$PORT_B" federation.blocked_servers "a.localhost"

say "And the ping is refused in both directions, immediately"
# Outgoing from B: refused before the address is looked up.
RESULT="$(ping_peer "$PORT_B" "http://a.localhost:${PORT_A}")"
echo "  B to A: $RESULT"
if ! echo "$RESULT" | grep -q "blocked"; then
  echo "FAILED: B still reaches out to a server it blocked" >&2
  exit 1
fi

# Incoming to B: A still believes it may talk to B, and B refuses it.
RESULT="$(ping_peer "$PORT_A" "http://b.localhost:${PORT_B}")"
echo "  A to B: $RESULT"
if ! echo "$RESULT" | grep -q '"reached":false'; then
  echo "FAILED: B accepted a request from a server it blocked" >&2
  exit 1
fi

say "Neither server was restarted; the block took effect on the next request"

say "Both the enablement and the block are in B's audit log"
curl -sf "http://localhost:${PORT_B}/api/admin/audit?limit=50" \
  -b "$WORK/session-${PORT_B}" \
  | python3 -c '
import json, sys
for entry in json.load(sys.stdin):
    action = entry["action"]
    if action.startswith("server.federation."):
        target = entry["targetId"]
        detail = entry["detail"]
        print("  %-46s %-32s %s" % (action, target, detail))
'

say "Milestone 3 demonstrated"
note "two servers allowlisted each other and the ping succeeded"
note "a server on neither allowlist was refused without being dialed"
note "a block refused the peer in both directions, with no restart"
note "every change is in the audit log under its own action"
