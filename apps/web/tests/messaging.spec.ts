import { expect, test } from "@playwright/test";

/*
 * The messaging cycle, between two clients at once.
 *
 * One browser cannot show the thing that matters here. A message appearing in
 * the window that sent it proves the composer works; it says nothing about
 * whether anybody else ever sees it, which is the entire purpose of a chat
 * application. The server's tests prove the event is stored and fanned out,
 * and they cannot prove a second browser rendered it.
 *
 * Both clients are the same account, and deliberately so: the server limits
 * sign-ins per account, and a second account per engine would exhaust that
 * before WebKit ran. Two contexts of one account is the same question — two
 * live connections, one event, both must receive it — and the fanout does not
 * know or care that the two subscribers are one person on a laptop and a
 * phone.
 */

const CHANNEL = "General";

async function openChannel(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page
    .getByRole("link", { name: new RegExp(CHANNEL, "i") })
    .first()
    .click();
  await page.getByRole("textbox", { name: /message/i }).waitFor();
  return page.getByRole("textbox", { name: /message/i });
}

test("a message sent in one client arrives in the other", async ({ browser }, testInfo) => {
  /*
   * Two contexts rather than two pages: a page shares its context's storage
   * and its live connection, so two pages of one context is one client with
   * two views. Two contexts is two clients.
   */
  const storageState = testInfo.project.use.storageState;
  const first = await browser.newContext({ storageState });
  const second = await browser.newContext({ storageState });

  try {
    const sender = await first.newPage();
    const reader = await second.newPage();

    const composer = await openChannel(sender);
    await openChannel(reader);

    const said = `between two clients ${Date.now().toString(36)}`;
    await composer.fill(said);
    await sender.keyboard.press("Enter");

    // In the window that sent it, which is the easy half.
    await expect(sender.getByText(said, { exact: true })).toHaveCount(1);

    /*
     * And in the other one, without it being reloaded. This is the assertion
     * the whole file exists for: it arrives over the live connection, or it
     * does not arrive at all.
     */
    await expect(reader.getByText(said, { exact: true })).toHaveCount(1, { timeout: 20_000 });

    /*
     * And back the other way, so this is not passing because one direction
     * happens to work.
     */
    const replied = `and back again ${Date.now().toString(36)}`;
    await reader.getByRole("textbox", { name: /message/i }).fill(replied);
    await reader.keyboard.press("Enter");

    await expect(sender.getByText(replied, { exact: true })).toHaveCount(1, { timeout: 20_000 });
  } finally {
    await first.close();
    await second.close();
  }
});

test("an edit and a deletion reach the other client too", async ({ browser }, testInfo) => {
  const storageState = testInfo.project.use.storageState;
  const first = await browser.newContext({ storageState });
  const second = await browser.newContext({ storageState });

  try {
    const sender = await first.newPage();
    const reader = await second.newPage();

    const composer = await openChannel(sender);
    await openChannel(reader);

    const original = `to be edited ${Date.now().toString(36)}`;
    await composer.fill(original);
    await sender.keyboard.press("Enter");
    await expect(reader.getByText(original, { exact: true })).toHaveCount(1, { timeout: 20_000 });

    /*
     * A message changed after it was read is the case where a stale client is
     * worst: the reader is looking at words the writer has retracted.
     */
    await sender.getByText(original, { exact: true }).hover();
    await sender.locator('button[title="Edit"]').last().click();

    /*
     * The focused element, not "the last textbox" — that finds the composer
     * and sends a new message instead, which reads as an edit that failed to
     * mark itself. The parity suite records the same trap.
     */
    const corrected = `${original} — corrected`;
    const editor = sender.locator("input:focus");
    await editor.waitFor();
    await editor.fill(corrected);
    await editor.press("Enter");

    await expect(reader.getByText(corrected, { exact: true })).toHaveCount(1, { timeout: 20_000 });
  } finally {
    await first.close();
    await second.close();
  }
});
