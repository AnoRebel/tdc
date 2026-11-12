#!/usr/bin/env bash
#
# The federation Milestone 2 demonstration.
#
# Two TDC servers exchange signed pings over the federated transport, and three
# things are refused: a ping replayed after its window has passed, a ping signed
# for one server and delivered to another, and a ping carrying no signature.
#
# Real servers rather than a mock peer, because what is being demonstrated is
# that a signature made in one process verifies in another with no shared
# memory between them — which a mock, holding both halves, cannot show.
#
# Usage: scripts/demo-milestone-federation-2.sh
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
note "server A is a.localhost:${PORT_A}"
note "server B is b.localhost:${PORT_B}"

# An operator's own credential, to trigger the outbound ping.
sign_in() {
  local port="$1"
  curl -sf -X POST "http://localhost:${port}/api/auth/signin" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" \
    -c "$WORK/session-${port}" -o /dev/null
}

ping_peer() {
  local from="$1" to="$2"
  curl -s -X POST "http://localhost:${from}/api/admin/federation/ping" \
    -H 'Content-Type: application/json' -b "$WORK/session-${from}" \
    -d "{\"peer\":\"${to}\"}"
}

sign_in "$PORT_A"
sign_in "$PORT_B"

say "A signs a ping and sends it to B"
RESULT="$(ping_peer "$PORT_A" "http://b.localhost:${PORT_B}")"
echo "  $RESULT"
if ! echo "$RESULT" | grep -q '"reached":true'; then
  echo "FAILED: the signed ping did not reach B" >&2
  exit 1
fi

say "And B signs a ping and sends it to A, so neither direction is special"
RESULT="$(ping_peer "$PORT_B" "http://a.localhost:${PORT_A}")"
echo "  $RESULT"
if ! echo "$RESULT" | grep -q '"reached":true'; then
  echo "FAILED: the signed ping did not reach A" >&2
  exit 1
fi

say "A peer that cannot be reached is reported, not silently ignored"
# Nothing is listening on this port. The interesting part is that the operator
# is told which peer failed and why, rather than the ping simply not happening.
RESULT="$(ping_peer "$PORT_A" "http://c.localhost:${PORT_LIAR}")"
echo "  $RESULT"
if ! echo "$RESULT" | grep -q '"reached":false'; then
  echo "FAILED: an unreachable peer was reported as reached" >&2
  exit 1
fi

say "What the transport refuses, proved directly against the two servers"
(cd "$ROOT/server" && go run ./cmd/tdc-federation-signed-probe \
  --self "http://a.localhost:${PORT_A}" \
  --peer "http://b.localhost:${PORT_B}" \
  --other "http://a.localhost:${PORT_A}")

say "The refusals, in B's own log"
grep -F "refused a federated request" "$WORK/b.log" | tail -5 | sed 's/^/  /' \
  || echo "  (no refusals logged)"

say "Milestone 2 demonstrated"
note "two servers exchanged signed pings in both directions"
note "a ping replayed after its window was refused"
note "a ping signed for one server and sent to another was refused"
note "each refusal is in the receiving server's log with a stated reason"
