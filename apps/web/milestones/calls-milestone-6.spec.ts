import { expect, test, type Browser, type Page } from "@playwright/test";

import {
  callWindow,
  camerasArriving,
  chosenRoutes,
  hearsVoices,
  inPage,
  signIn,
  skipTours,
  watchPeerConnections,
} from "./helpers";

/*
 * Calls Milestone 6: a four-person call carried as a mesh, with the
 * forwarding unit off, and a six-person call carried through it.
 *
 * Run by scripts/demo-milestone-calls-6.sh inside a network namespace of its
 * own, where the server's forwarding unit and every browser share one
 * address. Six people, each in a browser context of their own with a fake
 * camera and microphone, everybody's camera on.
 *
 * "Receives media from the others" is each other person's sound arriving on
 * every page, counted from the connections' own statistics. Sound rather
 * than pictures because the client steps cameras down — and pauses them —
 * when its estimate of the connection says so, and with a dozen fake cameras
 * encoding on one loaded machine that estimate can fall for reasons no real
 * network has; sound is never stepped down. How many cameras arrived is
 * reported alongside.
 */

const run = Date.now().toString(36);
const people = ["founder", "neema", "juma", "zawadi", "baraka", "amani"];

async function signedIn(browser: Browser, username: string): Promise<Page> {
  const page = await (await browser.newContext()).newPage();
  await watchPeerConnections(page);
  await signIn(page, username);
  await skipTours(page);
  return page;
}

/** A channel of the founder's, with the others in it. */
async function channel(pages: Page[], name: string): Promise<string> {
  const [founder, ...others] = pages;
  const created = await inPage(founder!, "POST", "/api/rooms", { slug: name, name });
  expect(created.status, `making the channel: ${created.text}`).toBeLessThan(300);
  const roomId = (JSON.parse(created.text) as { id: string }).id;
  for (const page of others) {
    expect((await inPage(page, "POST", `/api/rooms/${roomId}/join`)).status).toBeLessThan(300);
  }
  return roomId;
}

/** Everybody into the room's call with their camera on, the founder first. */
async function everybodyJoins(pages: Page[], roomId: string) {
  const [founder, ...others] = pages;
  for (const page of pages) await page.goto(`/rooms/${roomId}`);
  await founder!.getByRole("button", { name: "Start a call" }).click();
  await callWindow(founder!).getByRole("button", { name: "Turn camera on" }).click();
  for (const page of others) {
    await page
      .locator("[data-call-banner]")
      .getByRole("button", { name: "Join with video" })
      .click();
    await expect(callWindow(page).getByRole("button", { name: "Leave", exact: true })).toBeVisible({
      timeout: 30_000,
    });
  }
}

/** How the server says the call is carried. */
async function mode(page: Page, roomId: string): Promise<string> {
  const call = JSON.parse((await inPage(page, "GET", `/api/rooms/${roomId}/call`)).text) as {
    mode?: string;
  };
  return call.mode ?? "mesh";
}

async function everybodyLeaves(pages: Page[]) {
  for (const page of pages) {
    await callWindow(page).getByRole("button", { name: "Leave", exact: true }).click();
    await expect(callWindow(page)).toHaveCount(0);
  }
}

let pages: Page[] = [];

test.beforeAll(async ({ browser }) => {
  pages = [];
  for (const username of people) pages.push(await signedIn(browser, username));
});

test.afterAll(async () => {
  for (const page of pages) await page.context().close();
});

test("four people, each sending to each, with the forwarding unit off", async () => {
  const four = pages.slice(0, 4);
  const set = await inPage(four[0]!, "PUT", "/api/admin/settings/calls.sfu", { value: "false" });
  expect(set.status).toBe(204);
  const roomId = await channel(four, `mesh-${run}`);
  await everybodyJoins(four, roomId);

  for (const page of four) await hearsVoices(page, 3);
  expect(await mode(four[0]!, roomId)).toBe("mesh");
  // A connection to each of the other three, and none to the server: each
  // one's far end is another person's browser.
  for (const page of four) {
    await expect.poll(async () => (await chosenRoutes(page)).length).toBe(3);
  }
  const cameras = await Promise.all(four.map(camerasArriving));
  // eslint-disable-next-line no-console -- what the demonstration saw, for its summary
  console.log(
    `  mesh: each of 4 hears the other 3 over 3 direct connections; cameras showing: ${cameras.join(", ")}`,
  );

  await everybodyLeaves(four);
});

test("six people through the forwarding unit", async () => {
  const set = await inPage(pages[0]!, "PUT", "/api/admin/settings/calls.sfu", { value: "true" });
  expect(set.status).toBe(204);
  const roomId = await channel(pages, `forwarded-${run}`);
  await everybodyJoins(pages, roomId);

  // The fifth join took the call past the mesh limit of four: it moved to the
  // unit, and everybody in it was told.
  expect(await mode(pages[0]!, roomId)).toBe("sfu");
  for (const page of pages) {
    await expect(callWindow(page)).toContainText("This call now goes through the server", {
      timeout: 30_000,
    });
  }
  for (const page of pages) await hearsVoices(page, 5, 90_000);
  // One connection each, to the server, carrying all five.
  for (const page of pages) {
    await expect.poll(async () => (await chosenRoutes(page)).length, { timeout: 30_000 }).toBe(1);
  }
  const cameras = await Promise.all(pages.map(camerasArriving));
  // eslint-disable-next-line no-console -- what the demonstration saw, for its summary
  console.log(
    `  forwarded: each of 6 hears the other 5 over one connection, to the server; cameras showing: ${cameras.join(", ")}`,
  );

  await everybodyLeaves(pages);
});
