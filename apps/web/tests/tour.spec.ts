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
