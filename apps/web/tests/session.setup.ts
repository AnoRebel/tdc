import { expect, test as setup } from "@playwright/test";

import { SESSION } from "../playwright.config";

/*
 * One sign-in, reused by every check.
 *
 * Not a convenience: the server rate-limits sign-in attempts, and a suite that
 * signed in per test tripped that limit and then reported the application as
 * broken. The limit is right — an endpoint accepting unlimited attempts is a
 * password guesser's front door — so the suite is what changes.
 *
 * The tour is dismissed here too, once, so the checks that follow see the
 * application rather than an overlay over it.
 */
setup("sign in once", async ({ page }) => {
  await page.goto("/signin");
  await page.getByRole("button", { name: "Sign in" }).waitFor();

  await page.getByLabel("Username or email").fill("founder");
  await page.getByLabel("Password").fill("correct horse battery staple");
  await page.getByRole("button", { name: "Sign in" }).click();

  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));

  /*
   * The introduction, given to a first arrival.
   *
   * Dismissed with Escape rather than by reaching into storage, so when it does
   * appear this also asserts that somebody can leave the tour from the
   * keyboard.
   *
   * Whether it appears at all depends on the account, not the browser: the
   * record of having seen it is `users.tour_progress` on the server. So the
   * first engine to run this dismisses it and every engine after finds no
   * tour — as does a second run against the same database. Waiting for it
   * unconditionally made this pass exactly once per fresh database, which is
   * the kind of green that stops meaning anything.
   */
  const tour = page.locator(".driver-popover");
  if (await tour.isVisible().catch(() => false)) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }

  await page.context().storageState({ path: SESSION });
});
