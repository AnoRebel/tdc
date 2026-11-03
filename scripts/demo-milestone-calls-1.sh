#!/usr/bin/env bash
#
# Calls Milestone 1: an operator turns calling on, two members of a channel
# hold a video call, one mutes and the other sees it, one leaves and the call
# ends in the conversation's history — and, with a TURN relay, a call between
# two networks that cannot reach each other connects.
#
# Everything runs inside a network namespace of its own (unshare, no root),
# the "core", which holds the server, the relay and Playwright. The first call
# is between two browsers in the core. For the second, two more namespaces —
# network A and network B — each hang off the core by a virtual cable, and the
# core forwards nothing between cables: A and B each reach the server and the
# relay, and cannot reach each other. A browser runs in each.
#
# The relay is pion/turn (MIT), already among the server's dependencies, built
# here into a small program that takes the shared-secret credentials TDC mints
# (coturn's use-auth-secret scheme) — standing in for the compose file's coturn,
# which needs root or Docker. The server is configured as docs/hosting.md says:
# TDC_CALLS_TURN_SECRET in its environment, calls.turn_urls set by the operator.
#
# Usage: scripts/demo-milestone-calls-1.sh   (SKIP_WEB_BUILD=1 to reuse a build)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${TDC_DEMO_PORT:-8521}"
DATABASE_SERVER="${TDC_DEMO_DATABASE_SERVER:-postgres://tdc:tdc@127.0.0.1:55433}"
DATABASE="tdc_cb_calls1"
PASSWORD="correct horse battery staple"
# The core's own address, which both networks route to.
CORE=10.9.0.1

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }

if [[ "${1:-}" == "--inside" ]]; then
  WORK="$2"
  ip link set lo up
  # An address WebRTC will gather (it skips loopback, and any interface
  # without a route out), and the database bridged in.
  ip link add core0 type dummy
  ip addr add "$CORE/32" dev core0
  ip link set core0 up
  ip route add default dev core0
  # Nothing crosses the core from one network to another.
  echo 0 >/proc/sys/net/ipv4/ip_forward
  socat TCP-LISTEN:55433,bind=127.0.0.1,fork,reuseaddr "UNIX-CONNECT:$WORK/pg.sock" &
  pids=($!)

  # Networks A and B: a process holds each namespace; a veth pair joins it to
  # the core; its default route is the core.
  network() {
    local name="$1" subnet="$2"
    unshare -n sleep infinity &
    local holder=$!
    pids+=("$holder")
    sleep 0.2
    ip link add "v$name" type veth peer name eth0 netns "$holder"
    ip addr add "10.$subnet.0.1/24" dev "v$name"
    ip link set "v$name" up
    nsenter -t "$holder" -n sh -c "ip link set lo up
      ip addr add 10.$subnet.0.2/24 dev eth0 && ip link set eth0 up
      ip route add default via 10.$subnet.0.1"
    # The page is opened at localhost, as everywhere else in this demo, so the
    # browser treats it as secure and lets it use the camera.
    nsenter -t "$holder" -n socat "TCP-LISTEN:${PORT},bind=127.0.0.1,fork,reuseaddr" "TCP:${CORE}:${PORT}" &
    pids+=($!)
    eval "HOLDER_$name=$holder"
  }
  network a 1
  network b 2

  export TDC_CALLS_TURN_SECRET
  TDC_CALLS_TURN_SECRET="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  "$WORK/relay" -listen "$CORE:3478" -relay-ip "$CORE" >"$WORK/relay.log" 2>&1 &
  pids+=($!)

  cd "$WORK"
  TDC_DATABASE_URL="${DATABASE_SERVER}/${DATABASE}?sslmode=disable" \
  TDC_SERVER_ADDRESS="0.0.0.0:${PORT}" \
  TDC_SERVER_CANONICAL_ADDRESS="http://localhost:${PORT}" \
  TDC_SERVER_SECURE_COOKIES=false \
  TDC_SERVER_WEB_ROOT="$WORK/web" \
  TDC_STORAGE_DIRECTORY="$WORK/attachments" \
    "$WORK/tdc-server" >"$WORK/server.log" 2>&1 &
  pids+=($!)
  trap 'kill "${pids[@]}" 2>/dev/null || true' EXIT
  for _ in $(seq 1 120); do
    curl -sf "http://localhost:${PORT}/api/health" >/dev/null 2>&1 && break
    sleep 1
  done
  curl -sf -X POST "http://localhost:${PORT}/api/setup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"founder\",\"password\":\"${PASSWORD}\"}" >/dev/null \
    || { tail -30 "$WORK/server.log"; exit 1; }
  curl -sf -X POST "http://localhost:${PORT}/api/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"neema\",\"password\":\"${PASSWORD}\"}" >/dev/null

  say "The two networks"
  for name in a b; do
    holder="HOLDER_$name"
    other=$([[ $name == a ]] && echo 10.2.0.2 || echo 10.1.0.2)
    nsenter -t "${!holder}" -n curl -sf --max-time 3 "http://localhost:${PORT}/api/health" >/dev/null \
      || { echo "network $name cannot reach the server" >&2; exit 1; }
    # A timeout, not a refusal: nothing comes back at all.
    rc=0
    nsenter -t "${!holder}" -n curl -s --max-time 3 "telnet://${other}:9" </dev/null >/dev/null 2>&1 || rc=$?
    [[ $rc -eq 28 ]] || { echo "network $name can reach the other network" >&2; exit 1; }
    note "network $name ($([[ $name == a ]] && echo 10.1.0.2 || echo 10.2.0.2)) reaches the server and the relay at ${CORE}; ${other} does not answer"
  done

  say "Two calls: in one network, and between the two through the relay"
  cd "$ROOT/apps/web"
  TDC_TEST_URL="http://localhost:${PORT}" \
  TDC_NETNS_A="$HOLDER_a" TDC_NETNS_B="$HOLDER_b" \
  TDC_RELAY_URL="turn:${CORE}:3478?transport=udp" \
    bunx playwright test -c playwright.calls-b.config.ts --project=calls-1 --reporter=list \
    || { tail -n 20 "$WORK/server.log" "$WORK/relay.log"; exit 1; }
  exit 0
fi

WORK="$(mktemp -d)"
cleanup() {
  [[ -n "${BRIDGE:-}" ]] && kill "$BRIDGE" 2>/dev/null || true
  psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null 2>&1 || true
  if [[ -z "${KEEP_DEMO:-}" ]]; then rm -rf "$WORK"; else echo "kept: logs in $WORK"; fi
}
trap cleanup EXIT

say "Building the server, the relay and the web client from this checkout"
(cd "$ROOT/server" && go build -o "$WORK/tdc-server" ./cmd/tdc-server)
# The relay: a few lines over pion/turn, built offline from the module cache
# the server's own build filled, at the version the server's go.sum pins.
mkdir -p "$WORK/relay-src"
cat >"$WORK/relay-src/main.go" <<'GO'
// A TURN relay for the demonstration: pion/turn, with the shared-secret
// credentials coturn's use-auth-secret takes, which is what TDC mints.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/pion/turn/v5"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:3478", "UDP address to answer on")
	relayIP := flag.String("relay-ip", "", "address relayed media is sent from")
	flag.Parse()
	secret := os.Getenv("TDC_CALLS_TURN_SECRET")
	if secret == "" || *relayIP == "" {
		log.Fatal("TDC_CALLS_TURN_SECRET and -relay-ip are needed")
	}
	conn, err := net.ListenPacket("udp4", *listen)
	if err != nil {
		log.Fatal(err)
	}
	server, err := turn.NewServer(turn.ServerConfig{
		Realm:       "tdc",
		AuthHandler: turn.LongTermTURNRESTAuthHandler(secret, nil),
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: conn,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				// Sent from the address it is known by, so the far side sees
				// relayed packets come from the relay candidate it was given.
				RelayAddress: net.ParseIP(*relayIP), Address: *relayIP,
			},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("relay answering on %s, relaying from %s", *listen, *relayIP)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	_ = server.Close()
}
GO
turn_version="$(awk '$1 == "github.com/pion/turn/v5" {print $2}' "$ROOT/server/go.mod")"
printf 'module demorelay\n\ngo 1.25\n\nrequire github.com/pion/turn/v5 %s\n' "$turn_version" >"$WORK/relay-src/go.mod"
cp "$ROOT/server/go.sum" "$WORK/relay-src/go.sum"
(cd "$WORK/relay-src" && GOWORK=off GOFLAGS=-mod=mod GOPROXY="${GOPROXY:-off}" go build -o "$WORK/relay" .)
if [[ -z "${SKIP_WEB_BUILD:-}" || ! -f "$ROOT/apps/web/.output/public/index.html" ]]; then
  (cd "$ROOT/apps/web" && flock /tmp/tdc-web-build.lock env NUXT_PUBLIC_API_BASE="" bunx nuxt generate >"$WORK/generate.log" 2>&1) \
    || { tail -30 "$WORK/generate.log"; exit 1; }
fi
# A copy, taken while nobody is building: the checkout's build may be
# regenerated while this runs, and a page holding the old one's chunk names
# would break.
flock /tmp/tdc-web-build.lock cp -r "$ROOT/apps/web/.output/public" "$WORK/web"

psql "${DATABASE_SERVER}/postgres" -q -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE);" >/dev/null
psql "${DATABASE_SERVER}/postgres" -q -c "CREATE DATABASE ${DATABASE};" >/dev/null
socat "UNIX-LISTEN:$WORK/pg.sock,fork" "TCP:${DATABASE_SERVER##*@}" &
BRIDGE=$!

say "In a network namespace of its own: the server, a relay, and two networks"
unshare -rn "$0" --inside "$WORK" || {
  printf '\n\033[31mCalls Milestone 1 was not demonstrated\033[0m\n' >&2
  exit 1
}

say "Calls Milestone 1 demonstrated"
note "the operator turned calls on from Administration; before that no call button showed"
note "founder and neema held a video call, each receiving the other's camera"
note "neema muted, and the founder saw the mark on her tile; she left, and he saw her go"
note "the call's start and end are lines in the conversation, for both of them"
note "with TDC_CALLS_TURN_SECRET and calls.turn_urls set, two networks that cannot"
note "reach each other held a video call, each side's connection through the relay"
