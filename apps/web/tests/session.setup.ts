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
   * Dismissed with Escape rather than by reaching into storage, so this also
   * asserts — every run — that somebody can leave the tour from the keyboard.
   */
  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 8000 });
  await page.keyboard.press("Escape");
  await expect(tour).toHaveCount(0);

  // The tour's own record travels with the session, so it stays dismissed.
  await page.context().storageState({ path: SESSION });
});
