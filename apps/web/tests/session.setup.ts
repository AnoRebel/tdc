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
  await page.getByRole("button", { name: "Sign in", exact: true }).waitFor();

  await page.getByLabel("Username or email").fill("founder");
  await page.getByLabel("Password").fill("correct horse battery staple");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();

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

  /*
   * Waited for rather than asked about.
   *
   * isVisible() answers immediately, and on a fresh database the tour has not
   * rendered yet when it is asked — so the dismissal was skipped and the tour
   * appeared in every test that followed, trapping Tab inside its own two
   * buttons. Three checks failed that way and none of them was about the tour.
   *
   * A short wait that is allowed to find nothing: on a database where this
   * account has already seen it, there is genuinely nothing to dismiss.
   */
  await tour.waitFor({ state: "visible", timeout: 5000 }).catch(() => {});
  if (await tour.count()) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }

  await page.context().storageState({ path: SESSION });
});
