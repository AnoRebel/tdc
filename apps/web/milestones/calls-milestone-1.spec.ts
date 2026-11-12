import { chmodSync, mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { chromium, expect, test, type Browser, type Page } from "@playwright/test";

import {
  callWindow,
  chosenRoutes,
  type ChosenRoute,
  inPage,
  seesCameras,
  signIn,
  skipTours,
  watchPeerConnections,
} from "./helpers";

/*
 * Calls Milestone 1: an operator turns calling on, two members of a channel
 * hold a video call, one mutes and the other sees it, one leaves and the call
 * ends in the conversation's history — and, with a TURN relay, a call between
 * two networks that cannot reach each other connects.
 *
 * Run by scripts/demo-milestone-calls-1.sh inside a network namespace of its
 * own. The second test's browsers each run in a namespace of their own besides
 * (TDC_NETNS_A and TDC_NETNS_B, processes holding them), which reach the
 * server and the relay and not each other.
 */

const run = Date.now().toString(36);

/** Makes a channel as the founder and has the other person join it. */
async function channelFor(founder: Page, other: Page, name: string): Promise<string> {
  const created = await inPage(founder, "POST", "/api/rooms", { slug: name, name });
  expect(created.status, `making the channel: ${created.text}`).toBeLessThan(300);
  const roomId = (JSON.parse(created.text) as { id: string }).id;
  const joined = await inPage(other, "POST", `/api/rooms/${roomId}/join`);
  expect(joined.status, "the other person joining the channel").toBeLessThan(300);
  return roomId;
}

/** Founder starts a video call; the other joins it with video; each sees the other. */
async function holdVideoCall(founder: Page, other: Page, roomId: string) {
  await founder.goto(`/rooms/${roomId}`);
  await other.goto(`/rooms/${roomId}`);
  await founder.getByRole("button", { name: "Start a call" }).click();
  await callWindow(founder).getByRole("button", { name: "Turn camera on" }).click();
  await other
    .locator("[data-call-banner]")
    .getByRole("button", { name: "Join with video" })
    .click();
  await expect(callWindow(other).getByRole("button", { name: "Leave", exact: true })).toBeVisible();
  await seesCameras(founder, 1);
  await seesCameras(other, 1);
}

test("an operator turns calls on, and two members hold a video call", async ({ browser }) => {
  const founder = await (await browser.newContext()).newPage();
  await signIn(founder, "founder");
  await skipTours(founder);

  // Off on a new server: no call button anywhere.
  const neema = await (await browser.newContext()).newPage();
  await signIn(neema, "neema");
  await skipTours(neema);
  const roomId = await channelFor(founder, neema, `call-${run}`);
  await founder.goto(`/rooms/${roomId}`);
  await expect(founder.getByRole("button", { name: "Start a call" })).toHaveCount(0);

  // The operator turns it on from Administration → What this server offers.
  await founder.goto("/admin");
  const calls = founder.getByRole("switch", { name: /^Voice and video calls/ });
  await expect(calls).not.toBeChecked({ timeout: 30_000 });
  // The visible switch is the label around the (visually hidden) input.
  await founder.locator("label", { has: calls }).click();
  await expect(calls).toBeChecked();

  await holdVideoCall(founder, neema, roomId);

  // Muting shows on the other side, on that person's tile.
  await callWindow(neema).getByRole("button", { name: "Mute", exact: true }).click();
  const neemaTile = callWindow(founder).locator("li", { hasText: "neema" });
  await expect(neemaTile.locator('[aria-label="Muted"]')).toBeVisible();

  // One leaves: the other sees them go.
  await callWindow(neema).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(callWindow(neema)).toHaveCount(0);
  await expect(neemaTile).toHaveCount(0);

  // The last one out ends the call, a line in the conversation for everybody.
  await callWindow(founder).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(callWindow(founder)).toHaveCount(0);
  for (const page of [founder, neema]) {
    await expect(page.getByRole("note").filter({ hasText: "started a call" })).toBeVisible();
    await expect(page.getByRole("note").filter({ hasText: "The call ended after" })).toBeVisible();
  }

  await neema.context().close();
  await founder.context().close();
});

/**
 * A Chromium whose every process lives in another network namespace: a
 * wrapper that enters it, then runs the browser Playwright would have.
 * Playwright talks to it over a pipe, which crosses namespaces untouched.
 */
async function browserIn(holder: string, name: string): Promise<Browser> {
  mkdirSync(test.info().outputDir, { recursive: true });
  const wrapper = join(test.info().outputDir, `chromium-in-${name}`);
  writeFileSync(
    wrapper,
    `#!/bin/sh\nexec nsenter -t ${holder} -n -- "${chromium.executablePath()}" "$@"\n`,
  );
  chmodSync(wrapper, 0o755);
  return chromium.launch({
    executablePath: wrapper,
    args: [
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
      "--disable-features=WebRtcHideLocalIpsWithMdns",
    ],
  });
}

test("with a relay, two networks that cannot reach each other hold a call", async ({ baseURL }) => {
  const [holderA, holderB, relay] = [
    process.env.TDC_NETNS_A,
    process.env.TDC_NETNS_B,
    process.env.TDC_RELAY_URL,
  ];
  test.skip(!holderA || !holderB || !relay, "run by scripts/demo-milestone-calls-1.sh");

  const networkA = await browserIn(holderA!, "a");
  const networkB = await browserIn(holderB!, "b");
  const founder = await (await networkA.newContext({ baseURL })).newPage();
  const neema = await (await networkB.newContext({ baseURL })).newPage();
  await watchPeerConnections(founder);
  await watchPeerConnections(neema);
  await signIn(founder, "founder");
  await skipTours(founder);
  await signIn(neema, "neema");
  await skipTours(neema);

  // Calls on (the first test turned them on from Administration; this one
  // stands on its own), and the operator points the server at the relay; the secret it shares with
  // the relay is in the server's configuration (TDC_CALLS_TURN_SECRET).
  expect(
    (await inPage(founder, "PUT", "/api/admin/features/calls", { enabled: true })).status,
  ).toBe(204);
  const set = await inPage(founder, "PUT", "/api/admin/settings/calls.turn_urls", {
    value: relay,
  });
  expect(set.status, `setting the relay: ${set.text}`).toBe(204);
  const ice = JSON.parse((await inPage(neema, "GET", "/api/calls/ice")).text) as {
    relay: boolean;
    iceServers: { urls: string[]; username?: string }[];
  };
  expect(ice.relay, "a relay offered, with credentials").toBe(true);
  expect(ice.iceServers.some((each) => each.username)).toBe(true);

  const roomId = await channelFor(founder, neema, `relay-${run}`);
  await holdVideoCall(founder, neema, roomId);

  // The connection goes through the relay: one side (at least) sends from an
  // address the relay gave it, and the other sees packets arriving from the
  // relay — as a relay candidate, or as a peer-reflexive one when they came
  // before the candidate itself did. Never one browser's own address to the
  // other's: neither network can reach the other.
  let routes: Record<string, ChosenRoute[]> = {};
  await expect
    .poll(
      async () => {
        routes = {
          "founder (A)": await chosenRoutes(founder),
          "neema (B)": await chosenRoutes(neema),
        };
        return Object.values(routes)
          .flat()
          .some((route) => route.local === "relay");
      },
      { timeout: 30_000 },
    )
    .toBe(true)
    .catch((cause: unknown) => {
      throw new Error(`the connection was not relayed: ${JSON.stringify(routes)}`, { cause });
    });
  for (const [who, chosen] of Object.entries(routes)) {
    expect(chosen.some((route) => route.local === "host" && route.remote === "host")).toBe(false);
    const described = chosen.map(
      (route) => `${route.local} ${route.localAddress} ↔ ${route.remote} ${route.remoteAddress}`,
    );
    // eslint-disable-next-line no-console -- what the demonstration saw, for its summary
    console.log(`  ${who}: ${described.join("; ")}`);
  }

  await callWindow(neema).getByRole("button", { name: "Leave", exact: true }).click();
  await callWindow(founder).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(founder.getByRole("note").filter({ hasText: "The call ended after" })).toBeVisible();

  await networkA.close();
  await networkB.close();
});
