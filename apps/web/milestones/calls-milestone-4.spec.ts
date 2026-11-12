import { expect, test, type Browser, type Page } from "@playwright/test";

import { registered, ringsArriving, seesTheOther, signIn, tokenFor, tourSeen } from "./helpers";

/*
 * Calls Milestone 4: calls across servers.
 *
 * Run by scripts/demo-milestone-calls-4.sh, which starts two servers, each
 * with federation on, trusting the other, and taking calls from other servers.
 * Asha, on A, rings Bakari on B by his address; he answers with his camera and
 * the two see each other. Then the ring is tried three times when it should
 * not arrive — with B's federated calling off, with Bakari's own setting
 * refusing other servers, and with A's federated calling off — and finally
 * once more with everything back on. Only that last one rings him: the live
 * connection to his browser is listened to throughout, so a ring that came
 * and went would still be counted.
 */

const A = process.env.TDC_TEST_URL_A ?? "http://localhost:8513";
const B = process.env.TDC_TEST_URL_B ?? "http://localhost:8514";
const SERVER_B = new URL(process.env.TDC_CANONICAL_B ?? "http://b.localhost:8514").hostname;

const run = Date.now().toString(36);
const ASHA = `asha${run}`;
const BAKARI = `bakari${run}`;
const ADDRESS = `${BAKARI}@${SERVER_B}`;

const incoming = (page: Page) => page.locator("[data-incoming-call]");
const callWindow = (page: Page) => page.locator("[data-call-window]");

/** A browser of its own on one server; the servers share a host, and so cookies. */
async function on(browser: Browser, baseURL: string) {
  return (await browser.newContext({ baseURL })).newPage();
}

/** Sets one of a server's settings, as its operator. */
async function setting(page: Page, token: string, key: string, value: string) {
  const response = await page.request.put(`/api/admin/settings/${key}`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { value },
  });
  expect(response.ok(), `${key} = ${value}: ${await response.text()}`).toBeTruthy();
}

/** Asha starts a call in her channel and rings Bakari by address from it. */
async function ringBakari(asha: Page, room: string) {
  await asha.goto(`/rooms/${room}`);
  await asha.getByRole("button", { name: "Start a call" }).click();
  const calls = callWindow(asha);
  await expect(calls.getByRole("button", { name: "Leave", exact: true })).toBeVisible();
  await calls.getByRole("button", { name: "Ring people" }).click();
  const field = calls.getByRole("textbox", { name: "Ring somebody on another server by address" });
  await field.fill(ADDRESS);
  await calls
    .locator("[data-testid=call-ring-address]")
    .getByRole("button", { name: "Ring" })
    .click();
  // All A can say: the ring left. B answers the same whether it rang or not.
  await expect(calls).toContainText(`Rung ${ADDRESS}, if they accept calls from other servers.`);
  return calls;
}

async function hangUp(page: Page) {
  await callWindow(page).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(callWindow(page)).toHaveCount(0);
}

/** Bakari's own setting: whether people on connected servers may ring him. */
async function bakariTakesCallsFromElsewhere(bakari: Page, yes: boolean) {
  await bakari.goto("/settings?tab=privacy");
  const box = bakari.locator("[data-call-privacy]").getByLabel("People on connected servers");
  const saved = bakari.waitForResponse((response) => response.url().endsWith("/api/me/privacy"));
  if (yes) await box.check();
  else await box.uncheck();
  expect((await saved).ok(), "saving his setting").toBe(true);
  await bakari.reload();
  await expect(box).toBeChecked({ checked: yes });
}

test("a person on one server rings a person on another, and only when both servers and he allow it", async ({
  browser,
}) => {
  test.setTimeout(420_000);

  // The people, and each server's operator.
  const setupA = await on(browser, A);
  await setupA.goto("/");
  const operatorA = await tokenFor(setupA, "founder");
  const ashaToken = await registered(setupA, ASHA);
  await tourSeen(setupA, ashaToken);
  const asAsha = { Authorization: `Bearer ${ashaToken}` };
  const setupB = await on(browser, B);
  await setupB.goto("/");
  const operatorB = await tokenFor(setupB, "founder");
  await tourSeen(setupB, await registered(setupB, BAKARI));

  // Asha's channel on A, where she calls from.
  const created = await setupA.request.post("/api/rooms", {
    headers: asAsha,
    data: { slug: `field-${run}`, name: `field-${run}` },
  });
  expect(created.ok(), await created.text()).toBeTruthy();
  const room = ((await created.json()) as { id: string }).id;

  const asha = await on(browser, A);
  await signIn(asha, ASHA);
  const bakari = await on(browser, B);
  const rings = ringsArriving(bakari);
  await signIn(bakari, BAKARI);

  // Asha rings Bakari by his address; on B, he answers with his camera.
  await ringBakari(asha, room);
  await callWindow(asha).getByRole("button", { name: "Turn camera on" }).click();
  await expect(incoming(bakari)).toBeVisible();
  await expect(incoming(bakari)).toContainText(ASHA);
  await incoming(bakari).getByRole("button", { name: "Video", exact: true }).click();
  await expect(incoming(bakari)).toHaveCount(0);
  await expect(
    callWindow(bakari).getByRole("button", { name: "Leave", exact: true }),
  ).toBeVisible();
  await seesTheOther(asha);
  await seesTheOther(bakari);
  await hangUp(bakari);
  await hangUp(asha);
  expect(rings.size, "the call that rang him").toBe(1);
  rings.clear();

  // B stops taking calls from other servers: A still sends the ring.
  await setting(setupB, operatorB, "calls.federated", "false");
  await ringBakari(asha, room);
  await hangUp(asha);
  await setting(setupB, operatorB, "calls.federated", "true");

  // Bakari refuses calls from other servers himself.
  await bakariTakesCallsFromElsewhere(bakari, false);
  await ringBakari(asha, room);
  await hangUp(asha);
  await bakariTakesCallsFromElsewhere(bakari, true);

  // A stops calls crossing servers: Asha is no longer offered an address to
  // ring, and the server refuses one asked for anyway, counting it back.
  await setting(setupA, operatorA, "calls.federated", "false");
  await asha.goto(`/rooms/${room}`);
  await asha.getByRole("button", { name: "Start a call" }).click();
  await expect(callWindow(asha).getByRole("button", { name: "Leave", exact: true })).toBeVisible();
  await callWindow(asha).getByRole("button", { name: "Ring people" }).click();
  await expect(callWindow(asha).getByText("Everybody here is already in the call.")).toBeVisible();
  await expect(callWindow(asha).locator("[data-testid=call-ring-address]")).toHaveCount(0);
  const answer = await asha.evaluate(
    async ({ path, address }) => {
      const response = await fetch(path, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ addresses: [address] }),
      });
      return (await response.json()) as { rung: string[]; notRung: number };
    },
    { path: `/api/rooms/${room}/call/ring`, address: ADDRESS },
  );
  expect(answer, "A refusing to ring elsewhere").toEqual({ rung: [], notRung: 1 });
  await hangUp(asha);
  await setting(setupA, operatorA, "calls.federated", "true");

  // Everything back on: this one rings him — and it is the only one that has.
  await ringBakari(asha, room);
  await expect(incoming(bakari)).toBeVisible();
  await expect(incoming(bakari)).toContainText("Incoming call");
  await expect(incoming(bakari)).toContainText(ASHA);
  expect(rings.size, "calls that rang him since the first").toBe(1);
  await incoming(bakari).getByRole("button", { name: "Decline", exact: true }).click();
  await hangUp(asha);
});
