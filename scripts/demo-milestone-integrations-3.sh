#!/usr/bin/env bash
#
# Outgoing webhooks, demonstrated.
#
# A local endpoint subscribes to a channel's messages and receives a signed
# delivery, verified with the guide's own code. The endpoint is then stopped:
# retries appear in the history with growing delays, and once past the limit
# the subscription turns itself off and its owner is notified. Finally a slow
# receiver is subscribed while a second member watches the channel over the
# live connection — and the messages reach them promptly regardless.
#
# Retry delays are shortened by setting, so the demonstration takes seconds
# rather than a day; the schedule's shape is the same.
#
# Usage: scripts/demo-milestone-integrations-3.sh
set -euo pipefail

PORT="${TDC_DEMO_PORT:-8205}"
RECEIVER_PORT="${TDC_DEMO_RECEIVER_PORT:-8206}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-integrations-common.sh"
BASE="http://localhost:${PORT}"
RECEIVER="http://localhost:${RECEIVER_PORT}/hook"

build_server
start_server m3 "$PORT"
FOUNDER="$(setup_founder "$PORT")"

say "An operator allows local receivers, and shortens the retry schedule for the demonstration"
setting "$PORT" "$FOUNDER" integrations.allow_private_addresses true
setting "$PORT" "$FOUNDER" integrations.webhook_retry_base_seconds 1
setting "$PORT" "$FOUNDER" integrations.webhook_max_attempts 4
setting "$PORT" "$FOUNDER" integrations.delivery_timeout_seconds 3
note "retries after 1s, doubling; abandoned and turned off after 4 attempts"

ROOM="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/rooms \
  '{"slug":"builds","name":"builds","visibility":"public"}')" | json '["id"]')"

# The receiver: the guide's verification function, and a log of what it saw.
doc_example verify-delivery "$PORT" >"$WORK/verify-delivery.ts"
cat >"$WORK/receiver.ts" <<EOF
import { appendFileSync, existsSync, readFileSync } from "node:fs";
import { verifyDelivery, verifyHMAC } from "./verify-delivery";

const keys = (await (await fetch("${BASE}/.well-known/tdc-delivery-keys")).json()).keys;
const secretFile = "${WORK}/secret";

Bun.serve({
  port: ${RECEIVER_PORT},
  async fetch(request) {
    const body = await request.text();
    // How slow to be, when asked: the demonstration's stand-in for a
    // receiver that is struggling.
    const delay = existsSync("${WORK}/slow") ? Number(readFileSync("${WORK}/slow", "utf8")) : 0;
    let verified = true;
    let reason = "";
    try {
      verifyDelivery(request.headers, body, keys);
      if (existsSync(secretFile)) verifyHMAC(request.headers, body, readFileSync(secretFile, "utf8").trim());
    } catch (error) {
      verified = false;
      reason = (error as Error).message;
    }
    const payload = JSON.parse(body);
    appendFileSync("${WORK}/received.jsonl", JSON.stringify({
      at: Date.now(), verified, reason, id: payload.id, subscription: payload.subscription,
      body: payload.event?.content?.body,
    }) + "\n");
    if (delay) await Bun.sleep(delay);
    return new Response(null, { status: verified ? 204 : 401 });
  },
});
EOF

start_receiver() {
  bun run --cwd "$WORK" receiver.ts >>"$WORK/receiver.log" 2>&1 &
  RECEIVER_PID=$!
  HELPER_PIDS="${HELPER_PIDS:-} ${RECEIVER_PID}"
  for _ in $(seq 1 40); do
    ss -ltn "sport = :${RECEIVER_PORT}" | grep -q LISTEN && return 0
    sleep 0.25
  done
  fail "the receiver did not start: $(cat "$WORK/receiver.log")"
}
start_receiver
touch "$WORK/received.jsonl"

say "The guide's subscribe request, pointed at the local receiver"
OUT="$(ROOM="$ROOM" TOKEN="$FOUNDER" DOC_SED="s#https://ci.example.test/hooks/tdc#${RECEIVER}#" \
  run_doc_example subscribe "$PORT" "$ROOT/docs/webhooks-and-apps.md")"
expect_doc_statuses "$OUT" "subscribing"
CREATED="$(grep -E '^\{' <<<"$OUT" | head -1)"
HOOK="$(json '["id"]' <<<"$CREATED")"
json '["secret"]' <<<"$CREATED" >"$WORK/secret"
note "subscription ${HOOK}, secret written to the receiver"

say "A message is posted, and a signed delivery arrives"
api "$PORT" "$FOUNDER" POST "/api/rooms/${ROOM}/messages" '{"body":"Build 41 passed"}' >/dev/null
for _ in $(seq 1 40); do grep -q "Build 41 passed" "$WORK/received.jsonl" && break; sleep 0.25; done
LINE="$(grep "Build 41 passed" "$WORK/received.jsonl" | head -1)" || fail "no delivery arrived"
[[ "$(json '["verified"]' <<<"$LINE")" == "True" ]] || fail "the delivery did not verify: ${LINE}"
note "received and verified by Ed25519 signature and by HMAC: $(json '["id"]' <<<"$LINE")"

say "The receiver is stopped, and a message is posted"
kill "$RECEIVER_PID"
wait "$RECEIVER_PID" 2>/dev/null || true
api "$PORT" "$FOUNDER" POST "/api/rooms/${ROOM}/messages" '{"body":"Build 42 failed"}' >/dev/null

say "Retries appear in the history with growing delays, until the subscription turns itself off"
for _ in $(seq 1 80); do
  STATE="$(body_of "$(api "$PORT" "$FOUNDER" GET "/api/me/webhooks/${HOOK}")" | json '["state"]')"
  [[ "$STATE" == "disabled" ]] && break
  sleep 0.25
done
[[ "$STATE" == "disabled" ]] || fail "the subscription was never turned off"
body_of "$(api "$PORT" "$FOUNDER" GET "/api/me/webhooks/${HOOK}")" | python3 -c '
import json, sys
from datetime import datetime
history = json.load(sys.stdin)
failed = sorted((a for a in history["attempts"] if a["outcome"] == "failed"), key=lambda a: a["at"])
if len(failed) != 4:
    print("  FAILED: %d failed attempts recorded, want 4" % len(failed)); raise SystemExit(1)
times = [datetime.fromisoformat(a["at"]) for a in failed]
gaps = [(b - a).total_seconds() for a, b in zip(times, times[1:])]
for attempt, gap in zip(failed, [None] + gaps):
    print("  attempt %d  %-7s %s%s" % (attempt["attempt"], attempt["outcome"], attempt.get("error", ""),
          "" if gap is None else "   (%.1fs after the one before)" % gap))
if not all(later > earlier for earlier, later in zip(gaps, gaps[1:])):
    print("  FAILED: the delays did not grow: %s" % gaps); raise SystemExit(1)
print("  turned off: %s" % history["disabledReason"])
' || fail "the history does not show retries with growing delays"

NOTIFIED="$(body_of "$(api "$PORT" "$FOUNDER" GET /api/me/notifications)")"
grep -q '"webhook_disabled"' <<<"$NOTIFIED" || fail "the owner was not notified: ${NOTIFIED}"
note "the owner's notifications include the subscription being turned off"

say "A slow receiver, while a second member watches the channel live"
echo 2000 >"$WORK/slow"
start_receiver
SLOW="$(body_of "$(api "$PORT" "$FOUNDER" POST /api/me/webhooks \
  "{\"roomId\":\"${ROOM}\",\"label\":\"slow\",\"url\":\"${RECEIVER}\",\"eventTypes\":[\"message.sent\"]}")" | json '["id"]')"
rm -f "$WORK/secret"
WATCHER="$(register "$PORT" amina)"
api "$PORT" "$WATCHER" POST "/api/rooms/${ROOM}/join" >/dev/null

cat >"$WORK/watch.ts" <<EOF
// A member's live connection, timing how long each message takes to reach it
// after the request that posted it began.
const socket = new WebSocket("${BASE/http/ws}/api/live", {
  headers: { Authorization: "Bearer ${WATCHER}" },
} as never);
const sentAt = new Map<string, number>();
const latencies: number[] = [];
const postTimes: number[] = [];

// Finished only when every message has both arrived and had its post timed.
function finishIfDone() {
  if (latencies.length === 5 && postTimes.length === 5) {
    console.log(JSON.stringify({ latencies, postTimes }));
    socket.close();
    process.exit(0);
  }
}

socket.addEventListener("message", async (frame) => {
  const message = JSON.parse(String(frame.data));
  if (message.type === "ready") {
    socket.send(JSON.stringify({ action: "subscribe", rooms: ["${ROOM}"] }));
    return;
  }
  if (message.type === "subscribed") {
    for (let i = 0; i < 5; i++) {
      const body = "live " + i;
      sentAt.set(body, performance.now());
      const started = performance.now();
      await fetch("${BASE}/api/rooms/${ROOM}/messages", {
        method: "POST",
        headers: { Authorization: "Bearer ${FOUNDER}", "Content-Type": "application/json" },
        body: JSON.stringify({ body }),
      });
      postTimes.push(performance.now() - started);
      finishIfDone();
      await Bun.sleep(300);
    }
    return;
  }
  const body = message.payload?.content?.body;
  if (message.type === "event" && sentAt.has(body)) {
    latencies.push(performance.now() - sentAt.get(body)!);
    finishIfDone();
  }
});
setTimeout(() => { console.log(JSON.stringify({ latencies, postTimes, timedOut: true })); process.exit(1); }, 20000);
EOF
TIMINGS="$(bun run --cwd "$WORK" watch.ts)" || fail "the live connection did not receive every message: ${TIMINGS}"
python3 -c '
import json, sys
timings = json.loads(sys.argv[1])
for i, (latency, post) in enumerate(zip(timings["latencies"], timings["postTimes"])):
    print("  message %d: posted in %4.0f ms, on the live connection %4.0f ms after posting began" % (i, post, latency))
worst = max(timings["latencies"] + timings["postTimes"])
if worst > 500:
    print("  FAILED: %.0f ms, beside a receiver taking 2000 ms" % worst); raise SystemExit(1)
' "$TIMINGS" || fail "a slow receiver slowed the channel"

for _ in $(seq 1 60); do
  [[ "$(grep -c '"live ' "$WORK/received.jsonl" || true)" -ge 5 ]] && break
  sleep 0.5
done
[[ "$(grep -c '"live ' "$WORK/received.jsonl" || true)" -ge 5 ]] ||
  fail "the slow receiver never received the messages"
note "the slow receiver got all five, two seconds apart — one at a time, off the channel's path"

say "Milestone demonstrated"
note "a signed delivery arrived and verified with the guide's code"
note "a stopped receiver was retried with growing delays, then turned off, and its owner told"
note "a slow receiver did not delay posting or live delivery to the channel's members"
