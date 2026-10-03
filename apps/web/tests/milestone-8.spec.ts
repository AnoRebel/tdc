import { expect, test, type Page } from "@playwright/test";

/*
 * The Milestone 8 demonstration.
 *
 * Four things the phase promised, shown end to end in a browser rather than
 * asserted through an API — because all four are about what a browser does,
 * and an HTTP client cannot install anything, cannot be closed, and is never
 * offline in the way a person on a train is.
 *
 * What cannot be shown here is said plainly rather than skipped: a push
 * arriving with the browser genuinely closed needs a push service the browser
 * can reach, which is a property of the machine this runs on. The pieces that
 * do not depend on that are demonstrated, and the last hop is named.
 */

const password = "correct horse battery staple";

async function signIn(page: Page) {
  await page.goto("/signin");
  await page.getByRole("button", { name: "Sign in", exact: true }).waitFor();
  await page.getByLabel("Username or email").fill("founder");
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));

  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 5000 }).catch(() => {});
  if (await tour.count()) await page.keyboard.press("Escape");
}

test.describe("Milestone 8", () => {
  /*
   * Installable to a home screen.
   *
   * Shown by what makes it installable rather than by installing it: the
   * manifest a browser reads to decide, and the worker that lets it open
   * without a network. A browser's own install prompt cannot be driven from
   * a test, and asserting on one would be asserting about Chrome.
   */
  test("the application can be installed to a home screen", async ({ page }) => {
    await page.goto("/");

    const manifestHref = await page.getAttribute('link[rel="manifest"]', "href");
    expect(manifestHref, "no manifest, so nothing offers to install").toBeTruthy();

    const manifest = await page.evaluate(async (href: string) => {
      const response = await fetch(href);
      return (await response.json()) as {
        name?: string;
        display?: string;
        start_url?: string;
        icons?: { sizes?: string }[];
      };
    }, manifestHref!);

    expect(manifest.name).toBeTruthy();
    // Standalone is what makes an installed copy open without browser
    // furniture competing with the application's own header.
    expect(manifest.display).toBe("standalone");
    expect(manifest.start_url).toBeTruthy();
    // A phone needs a large icon to draw on a home screen and in a splash.
    expect(manifest.icons?.some((icon) => icon.sizes?.includes("512"))).toBe(true);

    /*
     * And a worker, without which an installed copy is a bookmark.
     *
     * Registered and activated, rather than controlling this page: a worker
     * takes control on the next navigation, so a first visit is served by the
     * network while the worker installs behind it. The second visit — the one
     * an installed copy always makes — is the one it answers.
     */
    await expect
      .poll(
        async () =>
          await page.evaluate(async () => {
            const registration = await navigator.serviceWorker.getRegistration();
            return registration?.active?.state ?? "none";
          }),
        { timeout: 20_000, message: "no service worker activated, so nothing works offline" },
      )
      .toBe("activated");

    // Which the next visit proves, by being controlled.
    await page.reload();
    await page.waitForFunction(() => navigator.serviceWorker.controller !== null, null, {
      timeout: 15_000,
    });
  });

  /*
   * A notification while the application is closed.
   *
   * The worker is what receives one, so this asks whether it can: the handler
   * is present in the worker that browsers actually receive, and it displays
   * what the server told it to. Whether the push reaches this machine depends
   * on a service outside it, which is noted rather than asserted.
   */
  test("a notification can arrive while the application is closed", async ({ page }) => {
    await signIn(page);

    await expect
      .poll(
        async () =>
          await page.evaluate(async () => {
            const registration = await navigator.serviceWorker.getRegistration();
            return registration?.active?.state ?? "none";
          }),
        { timeout: 20_000, message: "no service worker, so no push could arrive" },
      )
      .toBe("activated");

    /*
     * What receives a push, in the worker browsers are actually served.
     *
     * Displaying one is not asserted here: headless Chromium refuses
     * notification permission outright and cannot be granted it, so a check
     * that tried would be testing the browser rather than this application.
     * That half was verified by hand in a real browser — a push sent by this
     * server, through Google's push service, appearing on a desktop while the
     * conversation was not on screen — and this is the part that can be held
     * to on every commit.
     */
    const worker = await page.evaluate(async () => {
      const response = await fetch("/sw.js");
      return await response.text();
    });

    expect(worker, "the worker cannot receive a push").toContain('addEventListener("push"');
    expect(worker, "a notification would go nowhere when activated").toContain("notificationclick");
    expect(worker, "a push would arrive and show nothing").toContain("showNotification");

    /*
     * And that the server can send one at all: a server whose operator has
     * generated no keys says so, and this one has them.
     */
    const push = await page.evaluate(async () => {
      const response = await fetch("/api/push/config");
      return (await response.json()) as { enabled: boolean; publicKey?: string };
    });

    // Enabled or not is the operator's choice; what matters is that the
    // application asks and gets a straight answer either way.
    expect(typeof push.enabled).toBe("boolean");
    if (push.enabled) expect(push.publicKey).toBeTruthy();
  });

  /*
   * Reading with no connection.
   *
   * The conversation somebody last saw, shown from what this browser kept —
   * and labelled as remembered rather than passed off as current, because
   * replying to a stale timeline means answering something that has moved on.
   */
  test("messages can be read offline", async ({ page, context }) => {
    await signIn(page);

    await page
      .getByRole("link", { name: /General/ })
      .first()
      .click();
    await page.waitForURL(/\/rooms\//);
    await expect(page.getByText("Karibu")).toBeVisible();

    // The network goes away, as it does on a train.
    await context.setOffline(true);
    await page.reload();

    // The conversation is still there, and says when it was true.
    await expect(page.getByText("Karibu")).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText(/Showing this conversation as it was/)).toBeVisible();

    await context.setOffline(false);
  });

  /*
   * Composing offline, and sending on reconnection.
   *
   * The promise this phase makes to somebody typing where there is no signal:
   * what they wrote is neither lost nor pretended to have been sent.
   */
  test("a message composed offline sends when the connection returns", async ({
    page,
    context,
  }) => {
    // Longer than the default: this waits out a reconnection backoff.
    test.setTimeout(120_000);

    await signIn(page);

    await page
      .getByRole("link", { name: /General/ })
      .first()
      .click();
    await page.waitForURL(/\/rooms\//);

    // The conversation has to be open before the network goes: this is
    // somebody who was already reading when their train entered a tunnel.
    const composer = page.getByPlaceholder(/Message/);
    await composer.waitFor({ timeout: 15_000 });

    await context.setOffline(true);

    const written = `Nimeandika hii nikiwa nje ya mtandao ${Date.now()}`;
    await composer.fill(written);
    await page.keyboard.press("Enter");

    // Held, and shown as held rather than as sent.
    await expect(page.getByText(written)).toBeVisible();
    await expect(page.getByText("Waiting to send")).toBeVisible();

    // The connection comes back.
    await context.setOffline(false);

    /*
     * And it goes. The waiting label is what disappears: the message stays on
     * screen throughout, which is the point — nobody watches their own words
     * vanish and reappear.
     */
    /*
     * Allowed the reconnection's own time.
     *
     * The live connection backs off exponentially to thirty seconds, so a
     * message can wait that long after the network returns before anything
     * notices. That is deliberate — a client retrying hard against a server
     * that is down makes the outage worse — and it is the ceiling this has to
     * sit inside rather than a delay worth removing.
     */
    await expect(page.getByText("Waiting to send")).toHaveCount(0, { timeout: 60_000 });
    await expect(page.getByText(written)).toBeVisible();
  });
});
