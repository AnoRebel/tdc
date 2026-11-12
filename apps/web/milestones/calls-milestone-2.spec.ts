import { expect, test, type Browser, type Page } from "@playwright/test";

import { registered, ringsArriving, seesTheOther, signIn, tourSeen } from "./helpers";

/*
 * Calls Milestone 2: ringing, and who may call whom.
 *
 * Run by scripts/demo-milestone-calls-2.sh against a fresh server with calls
 * and the directory on. Amina is signed in on two devices — one with a message
 * half written, the other in her settings, where she says only people she
 * shares a conversation with may ring her. Zuberi, a stranger to her, calls
 * from her profile and nothing rings; Baraka, her colleague in #design, calls
 * from the same profile and both her devices ring, beside what she was doing
 * rather than over it. She answers on one with her camera, the other stops,
 * and the two see each other.
 */

const run = Date.now().toString(36);
const AMINA = `amina${run}`;
const BARAKA = `baraka${run}`;
const ZUBERI = `zuberi${run}`;

async function device(
  browser: Browser,
  username: string,
): Promise<{ page: Page; rings: Set<string> }> {
  const context = await browser.newContext();
  const page = await context.newPage();
  const rings = ringsArriving(page);
  await signIn(page, username);
  return { page, rings };
}

const incoming = (page: Page) => page.locator("[data-incoming-call]");
const callWindow = (page: Page) => page.locator("[data-call-window]");

test("a colleague rings both devices from a profile; a stranger does not ring at all", async ({
  browser,
}) => {
  test.setTimeout(300_000);

  // The people, made through the API: the milestone is about ringing, not
  // registering. Amina has a directory profile her colleagues can open.
  const setup = await (await browser.newContext()).newPage();
  await setup.goto("/");
  const asAmina = { Authorization: `Bearer ${await registered(setup, AMINA)}` };
  const asBaraka = { Authorization: `Bearer ${await registered(setup, BARAKA)}` };
  const zuberiToken = await registered(setup, ZUBERI);
  for (const token of [asAmina, asBaraka]) await tourSeen(setup, token.Authorization.slice(7));
  await tourSeen(setup, zuberiToken);

  const own = await (await setup.request.get("/api/directory/me", { headers: asAmina })).json();
  const profile = await setup.request.put("/api/directory/me", {
    headers: asAmina,
    data: { ...own.profile, visibility: "members" },
  });
  expect(profile.ok(), `amina's profile: ${await profile.text()}`).toBeTruthy();

  // Baraka and Amina work together in #design; Zuberi is in nothing with her.
  const created = await setup.request.post("/api/rooms", {
    headers: asBaraka,
    data: { slug: `design-${run}`, name: `design-${run}` },
  });
  expect(created.ok(), await created.text()).toBeTruthy();
  const design = ((await created.json()) as { id: string }).id;
  expect((await setup.request.post(`/api/rooms/${design}/join`, { headers: asAmina })).ok()).toBe(
    true,
  );

  // Amina's first device: a message half written in #design.
  const laptop = await device(browser, AMINA);
  await laptop.page.goto(`/rooms/${design}`);
  const draft = laptop.page.getByRole("textbox", { name: /^Message/ });
  await draft.fill("Notes for the review: the header");

  // Her second device: Settings → Privacy, where she narrows who may call her.
  const phone = await device(browser, AMINA);
  await phone.page.goto("/settings?tab=privacy");
  const callPrivacy = phone.page.locator("[data-call-privacy]");
  const saved = phone.page.waitForResponse(
    (response) => response.request().method() !== "GET" && /privacy|calls/.test(response.url()),
  );
  await callPrivacy.getByLabel("People you share a conversation with").check();
  expect((await saved).ok(), "saving who may call her").toBe(true);
  await phone.page.reload();
  await expect(callPrivacy.getByLabel("People you share a conversation with")).toBeChecked();

  // Zuberi, a stranger, calls her from her profile. The call starts and
  // nobody is rung — counted back, never named, never explained.
  const zuberi = (await device(browser, ZUBERI)).page;
  await zuberi.goto(`/directory/people/${AMINA}`);
  const zuberisRing = zuberi.waitForResponse((response) => response.url().endsWith("/call/ring"));
  await zuberi.getByRole("button", { name: `Call ${AMINA}`, exact: true }).click();
  const refused = (await (await zuberisRing).json()) as { rung: string[]; notRung: number };
  expect(refused, "the stranger's ring").toEqual({ rung: [], notRung: 1 });
  await expect(
    callWindow(zuberi).getByRole("button", { name: "Leave", exact: true }),
  ).toBeVisible();
  await callWindow(zuberi).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(callWindow(zuberi)).toHaveCount(0);

  // Baraka, her colleague, calls her from the same profile, with his camera.
  const baraka = (await device(browser, BARAKA)).page;
  await baraka.goto(`/directory/people/${AMINA}`);
  const barakasRing = baraka.waitForResponse((response) => response.url().endsWith("/call/ring"));
  await baraka.getByRole("button", { name: `Video call ${AMINA}`, exact: true }).click();
  const rung = (await (await barakasRing).json()) as { rung: string[]; notRung: number };
  expect(rung.rung, "the colleague's ring").toHaveLength(1);
  expect(rung.notRung).toBe(0);

  // It rings on both her devices: one call — Baraka's. Had Zuberi's reached
  // her, it would have arrived first, on the same connection.
  for (const each of [laptop, phone]) {
    await expect(incoming(each.page)).toBeVisible();
    await expect(incoming(each.page)).toContainText("Incoming call");
    await expect(incoming(each.page)).toContainText(BARAKA);
    await expect(incoming(each.page)).not.toContainText(ZUBERI);
    expect(each.rings.size, "calls that rang this device").toBe(1);
  }

  // Beside what she was doing, not over it: a window that does not hold the
  // page, and the half-written message still hers to finish.
  await expect(incoming(laptop.page)).toHaveAttribute("aria-modal", "false");
  const uncovered = await draft.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const top = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
    return top === element || element.contains(top);
  });
  expect(uncovered, "the message being written is not covered").toBe(true);
  await draft.pressSequentially(" needs a second look.");
  await expect(draft).toHaveValue("Notes for the review: the header needs a second look.");

  // She answers on the laptop, with her camera; the phone stops ringing.
  await incoming(laptop.page).getByRole("button", { name: "Video", exact: true }).click();
  await expect(incoming(laptop.page)).toHaveCount(0);
  await expect(incoming(phone.page)).toHaveCount(0);
  await expect(callWindow(phone.page)).toHaveCount(0);

  // And they see each other.
  await seesTheOther(baraka);
  await seesTheOther(laptop.page);

  await callWindow(laptop.page).getByRole("button", { name: "Leave", exact: true }).click();
  await callWindow(baraka).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(callWindow(baraka)).toHaveCount(0);
});
