import { expect, test } from "@playwright/test";

/*
 * The introduction somebody meets on their first arrival, and leaving it.
 *
 * This has its own account, registered as the test runs. Whether the tour
 * appears is a property of the account rather than the browser — the record is
 * users.tour_progress on the server — so a shared account sees it exactly once
 * ever, and an assertion made against one is an assertion that stops running
 * the moment somebody else's test gets there first.
 *
 * A fresh account each run is what keeps this checking something. It is also
 * why the tour is not dismissed here through storage: somebody who cannot use
 * a mouse has to be able to leave, and that is the thing worth asserting.
 */

// Unique per run, because the account outlives the test.
const username = `tourist${Date.now().toString(36)}`;
const password = "correct horse battery staple";

test.use({ storageState: { cookies: [], origins: [] } });

test("the introduction can be left from the keyboard", async ({ page }) => {
  await page.goto("/register");
  await page.getByRole("button", { name: /create account/i }).waitFor();

  await page.getByLabel(/^username/i).fill(username);
  await page.getByLabel(/^password/i).fill(password);
  await page.getByRole("button", { name: /create account/i }).click();

  await page.waitForURL((url) => !url.pathname.startsWith("/register"));

  /*
   * It is shown on arrival rather than on request, so a first visit explains
   * itself. An account that never sees it has lost the explanation.
   */
  const tour = page.locator(".driver-popover");
  await expect(tour).toBeVisible({ timeout: 10000 });

  await page.keyboard.press("Escape");
  await expect(tour).toHaveCount(0);

  /*
   * And it stays gone. The record is on the account, so a reload is the honest
   * check: a tour that came back on every visit would be the application
   * explaining itself to somebody who has already read it.
   */
  await page.reload();
  await page
    .getByRole("button", { name: /search/i })
    .first()
    .waitFor();
  await expect(tour).toHaveCount(0);
});

/*
The introduction is not given where it has nothing to point at.

The chat tour names four controls, all of them on the conversation screen. The
layout it starts from is shared by settings, administration, reports and the
audit log — and on any of those every selector misses, so driver.js falls back
to an empty div at the centre of the screen. It then marks that div
aria-haspopup, which axe reports as a critical violation, and traps Tab inside
its own two buttons, so somebody navigating by keyboard on their first visit
can reach Next and Close and nothing else.

Neither was visible for a long time, because the suite dismisses the tour
before everything else runs: every other check sees the application as somebody
who has already been through it.
*/
test("the introduction is not given on a page it cannot describe", async ({ page }) => {
  const username = `settler${Date.now().toString(36)}`;
  const password = "correct horse battery staple";

  await page.goto("/register");
  await page.getByRole("button", { name: /create account/i }).waitFor();
  await page.getByLabel(/^username/i).fill(username);
  await page.getByLabel(/^password/i).fill(password);
  await page.getByRole("button", { name: /create account/i }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/register"));

  /*
   * Straight to settings without dismissing anything.
   *
   * The dismissal is the trap in writing this test: pressing Escape marks the
   * tour seen on the account, and it would then never appear anywhere — so
   * the test would pass with every guard removed. An account that has not
   * seen it, on a page the tour cannot describe, is the only state that
   * actually asks the question.
   */
  await page.goto("/settings");
  await page
    .getByRole("heading", { name: /settings/i })
    .first()
    .waitFor();

  // Long enough that a tour which was going to appear has.
  await page.waitForTimeout(3000);

  const tour = page.locator(".driver-popover");
  await expect(tour, "the chat tour was given on the settings page").toHaveCount(0);

  /*
   * And the empty div driver.js falls back to is not in the document.
   *
   * That element is the whole problem: it carries aria-haspopup on a plain
   * div, which axe reports as critical, and Tab is trapped in the popover
   * beside it.
   */
  await expect(page.locator("#driver-dummy-element")).toHaveCount(0);

  // Tab still goes somewhere, which it would not inside a trap.
  await page.keyboard.press("Tab");
  const focused = await page.evaluate(() => document.activeElement?.tagName ?? "none");
  expect(focused, "Tab reached nothing, which is what a focus trap looks like").not.toBe("none");
});
