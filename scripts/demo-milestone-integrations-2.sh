#!/usr/bin/env bash
#
# Signed deliveries, demonstrated.
#
# The published keys are fetched from a live server; the server sends a signed
# test delivery to a receiver; and the receiver verifies it with the
# documentation's own code, holding nothing but the published keys and the
# delivery itself. The same code then refuses the delivery with one byte of its
# body changed, and refuses it replayed outside the window.
#
# The receiver is the documented one, run as written, and the verification is
# the documented function, extracted from the guide rather than copied — so the
# guide cannot drift from what the server does without this failing.
#
# Usage: scripts/demo-milestone-integrations-2.sh
set -euo pipefail

PORT="${TDC_DEMO_PORT:-8202}"
RECEIVER_PORT="${TDC_DEMO_RECEIVER_PORT:-8203}"
CHECKER_PORT="${TDC_DEMO_CHECKER_PORT:-8204}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-integrations-common.sh"
BASE="http://localhost:${PORT}"

build_server
start_server m2 "$PORT"
FOUNDER="$(setup_founder "$PORT")"

say "The keys, fetched with no credential at all"
KEYS="$(curl -sf "${BASE}/.well-known/tdc-delivery-keys")" || fail "the keys are not published"
echo "$KEYS" | python3 -c '
import json, sys
document = json.load(sys.stdin)
assert document["algorithm"] == "ed25519", document
assert document["replayWindowSeconds"] == 300, document
assert document["keys"], "no keys published"
print("  %d key(s), %s, a %d-second window" % (len(document["keys"]), document["algorithm"], document["replayWindowSeconds"]))
' || fail "the published document is not what the guide describes"

say "A delivery to a private address is refused until the operator allows it"
REFUSED="$(api "$PORT" "$FOUNDER" POST /api/integrations/test-delivery \
  "{\"url\":\"http://localhost:${RECEIVER_PORT}/\"}")"
expect_status "$REFUSED" 403 "a test delivery to loopback, by default"
setting "$PORT" "$FOUNDER" integrations.allow_private_addresses true
note "integrations.allow_private_addresses is now true, as the hosting guide describes"

say "The documented receiver, run as written"
doc_example verify-delivery "$PORT" >"$WORK/verify-delivery.ts"
doc_example delivery-receiver "$PORT" | sed "s/port: 9000/port: ${RECEIVER_PORT}/" >"$WORK/receiver.ts"
[[ -s "$WORK/verify-delivery.ts" && -s "$WORK/receiver.ts" ]] || fail "the guide's examples are missing"
# Started directly rather than in a subshell, so the recorded process is bun
# itself and cleanup stops it rather than leaving it holding the port.
TDC_SERVER="$BASE" bun run --cwd "$WORK" receiver.ts >"$WORK/receiver.log" 2>&1 &
HELPER_PIDS="${HELPER_PIDS:-} $!"

# A second receiver, holding the same documented function, that also tries it
# against what an attacker could do with a captured delivery.
cat >"$WORK/checker.ts" <<EOF
import { createHash } from "node:crypto";
import { writeFileSync } from "node:fs";
import { verifyDelivery, verifyHMAC } from "./verify-delivery";

const keys = (await (await fetch("${BASE}/.well-known/tdc-delivery-keys")).json()).keys;

function outcome(check: () => void): string {
  try {
    check();
    return "accepted";
  } catch (error) {
    return "refused: " + (error as Error).message;
  }
}

Bun.serve({
  port: ${CHECKER_PORT},
  async fetch(request) {
    const body = await request.text();
    const tampered = body.replace("delivery.test", "delivery.tesT");
    const later = Date.now() + 10 * 60 * 1000;
    // An attacker who changes the body can change its digest too; only the
    // signature over the digest stands in the way.
    const forged = new Headers(request.headers);
    forged.set("content-digest", "sha-256=:" + createHash("sha256").update(tampered).digest("base64") + ":");
    const results = {
      original: outcome(() => verifyDelivery(request.headers, body, keys)),
      tampered: outcome(() => verifyDelivery(request.headers, tampered, keys)),
      forgedDigest: outcome(() => verifyDelivery(forged, tampered, keys)),
      replayed: outcome(() => verifyDelivery(request.headers, body, keys, later)),
      hmac: outcome(() => verifyHMAC(request.headers, body, "demo secret")),
      hmacWrongSecret: outcome(() => verifyHMAC(request.headers, body, "a guess")),
      hmacTampered: outcome(() => verifyHMAC(request.headers, tampered, "demo secret")),
    };
    writeFileSync("${WORK}/checked.json", JSON.stringify(results));
    return new Response(null, { status: 204 });
  },
});
EOF
bun run --cwd "$WORK" checker.ts >"$WORK/checker.log" 2>&1 &
HELPER_PIDS="${HELPER_PIDS:-} $!"

for port in "$RECEIVER_PORT" "$CHECKER_PORT"; do
  for _ in $(seq 1 30); do
    curl -s -o /dev/null "http://localhost:${port}/" -X POST 2>/dev/null && break
    sleep 0.3
  done
done
# Those probes carried no signature, so the documented receiver must have
# refused them — and must have accepted nothing yet.
grep -q "delivery.test" "$WORK/receiver.log" && fail "the receiver accepted an unsigned request"
rm -f "$WORK/checked.json"

say "The server sends a signed test delivery to the documented receiver"
# The documented request, with its placeholder address pointed at the receiver
# started above and nothing else changed.
OUT="$(TOKEN="$FOUNDER" DOC_SED="s#https://hooks.example.test/tdc#http://localhost:${RECEIVER_PORT}/#" \
  run_doc_example test-delivery "$PORT")"
expect_doc_statuses "$OUT" "the documented test-delivery request"
SENT="$(grep -E '^\{' <<<"$OUT" | head -1)"
DELIVERY_ID="$(json '["id"]' <<<"$SENT")"
[[ "$(json '["delivered"]' <<<"$SENT")" == "True" ]] ||
  fail "the documented receiver did not accept the delivery: ${SENT}"
sleep 0.5
grep -q "delivery.test ${DELIVERY_ID}" "$WORK/receiver.log" ||
  fail "the receiver did not log the verified delivery: $(cat "$WORK/receiver.log")"
note "verified and answered 204, logging: $(grep "delivery.test" "$WORK/receiver.log" | tail -1)"

say "The same documented function against the delivery, altered and replayed"
SENT="$(api "$PORT" "$FOUNDER" POST /api/integrations/test-delivery \
  "{\"url\":\"http://localhost:${CHECKER_PORT}/\",\"secret\":\"demo secret\"}")"
expect_status "$SENT" 200 "a second test delivery"
for _ in $(seq 1 20); do [[ -s "$WORK/checked.json" ]] && break; sleep 0.2; done
python3 - "$WORK/checked.json" <<'EOF' || fail "the documented verification judged something wrongly"
import json, sys
results = json.load(open(sys.argv[1]))
expected = {
    "original": "accepted",
    "tampered": "refused",
    "forgedDigest": "refused: the signature does not verify",
    "replayed": "refused",
    "hmac": "accepted",
    "hmacWrongSecret": "refused",
    "hmacTampered": "refused",
}
failed = False
for check, want in expected.items():
    got = results[check]
    ok = got.startswith(want)
    failed |= not ok
    print("  %-16s %s%s" % (check, got, "" if ok else "   <-- expected " + want))
raise SystemExit(1 if failed else 0)
EOF

say "Milestone demonstrated"
note "the keys were fetched from a live server with no credential"
note "a real signed delivery verified with the guide's code, from the keys and the delivery alone"
note "one changed byte, a replay outside the window, and a wrong secret were each refused"
