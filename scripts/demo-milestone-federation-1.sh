#!/usr/bin/env bash
#
# The federation Milestone 1 demonstration.
#
# Two TDC servers on different addresses publish their identities; the
# discovery client on one fetches, caches and verifies the other's; and a third
# instance claiming a domain it is not served from is refused.
#
# Three real servers rather than a mock peer, because the thing being
# demonstrated is that a server's identity is only worth the domain serving it
# — and a mock that answers whatever it is told to would demonstrate the
# opposite.
#
# Usage: scripts/demo-milestone-federation-1.sh
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

say "Two servers, on different addresses, with separate databases and keys"
start_server a "$PORT_A" "http://a.localhost:${PORT_A}"
start_server b "$PORT_B" "http://b.localhost:${PORT_B}"
enable_federation "$PORT_A"
enable_federation "$PORT_B"
note "server A is a.localhost:${PORT_A}"
note "server B is b.localhost:${PORT_B}"

say "Each publishes its identity, unauthenticated, at the well-known path"
for port in "$PORT_A" "$PORT_B"; do
  echo "  --- http://localhost:${port}/.well-known/tdc-federation"
  curl -sf "http://localhost:${port}/.well-known/tdc-federation" \
    | python3 -m json.tool | sed 's/^/  /'
done

say "The two servers hold genuinely different keys"
KEY_A="$(curl -sf "http://localhost:${PORT_A}/.well-known/tdc-federation" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["keys"][0]["publicKey"])')"
KEY_B="$(curl -sf "http://localhost:${PORT_B}/.well-known/tdc-federation" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["keys"][0]["publicKey"])')"
if [[ "$KEY_A" == "$KEY_B" ]]; then
  echo "FAILED: both servers published the same key — they are not independent" >&2
  exit 1
fi
note "A publishes ${KEY_A:0:16}…"
note "B publishes ${KEY_B:0:16}…"

say "A third instance claiming a domain it is not served from"
# Its canonical address names server A, but it answers on its own port. This is
# what an impersonation looks like: a perfectly well-formed document, served
# from the wrong place.
start_server liar "$PORT_LIAR" "http://b.localhost:${PORT_B}"
note "the impostor on c.localhost:${PORT_LIAR} claims to be b.localhost:${PORT_B}"
curl -sf "http://localhost:${PORT_LIAR}/.well-known/tdc-federation" \
  | python3 -m json.tool | sed 's/^/  /'

say "The discovery client: fetching, caching, verifying, and refusing"
(cd "$ROOT/server" && go run ./cmd/tdc-federation-probe \
  --peer "http://b.localhost:${PORT_B}" \
  --impostor "http://c.localhost:${PORT_LIAR}")

say "Milestone 1 demonstrated"
note "two servers published verifiable, distinct identities"
note "the discovery client fetched, cached and verified a peer's domain"
note "an instance claiming a domain it is not served from was refused"
