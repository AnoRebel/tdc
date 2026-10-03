import { expect, request as playwrightRequest, test } from "@playwright/test";

/*
 * What somebody cannot reach, reached from a browser.
 *
 * The server's own tests already prove each refusal at the HTTP boundary, and
 * they are the authority on it. What they cannot show is that the application
 * in front of them does not quietly hold a door open — a channel listed in a
 * sidebar it should not be in, a route that renders because the client
 * decided it may, a refusal the interface swallows and presents as an empty
 * room.
 *
 * So these ask the question from where an attacker sits: signed in as somebody
 * ordinary, in a real browser, against a channel they are not in.
 */

const password = "correct horse battery staple";

/*
 * One unprivileged account for the whole file, registered once.
 *
 * Registering per test trips the server's rate limiter — correctly, since an
 * endpoint that accepts unlimited registrations is a spam account factory —
 * and the suite then reports the application as broken when what broke was the
 * suite. The same lesson is written into session.setup.ts.
 *
 * Fresh per run rather than fixed, because the account outlives the test and a
 * second run must not collide with the first.
 */
const outsider = `outsider${Date.now().toString(36)}`;
let registered = false;

async function anOrdinaryPerson(page: import("@playwright/test").Page) {
  if (!registered) {
    await page.goto("/register");
    await page.getByRole("button", { name: /create account/i }).waitFor();
    await page.getByLabel(/^username/i).fill(outsider);
    await page.getByLabel(/^password/i).fill(password);
    await page.getByRole("button", { name: /create account/i }).click();
    await page.waitForURL((url) => !url.pathname.startsWith("/register"));
    registered = true;
  } else {
    await page.goto("/signin");
    await page.getByRole("button", { name: "Sign in", exact: true }).waitFor();
    await page.getByLabel(/username or email/i).fill(outsider);
    await page.getByLabel(/^password/i).fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await page.waitForURL((url) => !url.pathname.startsWith("/signin"));
  }

  // The introduction, if this is the first arrival for this account.
  const tour = page.locator(".driver-popover");
  if (await tour.isVisible().catch(() => false)) await page.keyboard.press("Escape");

  return outsider;
}

/*
 * One private channel for the whole file, for the same reason: each creation
 * is a signed-in request, and four of them is four sign-ins.
 */
let channel: { id: string; slug: string } | undefined;

/*
 * A private channel, created over the API by the founder.
 *
 * Over the API rather than through the interface because this is the fixture,
 * not the thing under test: a failure here should say the channel could not be
 * made, not fail a permission check for the wrong reason.
 */
/*
 * The private channel these test against, created once over the API.
 *
 * Created with the session the setup project stored rather than a fresh
 * sign-in: the server limits sign-ins to eight per account per quarter hour,
 * and spending them on fixtures is how a suite starts failing for reasons that
 * have nothing to do with the code.
 */
async function aPrivateChannel(baseURL: string) {
  if (channel) return channel;

  /*
   * Created with a bearer token rather than the stored browser session.
   *
   * tdc_session is a Secure cookie, so it is never sent over the plain HTTP
   * this suite runs against and an API request context built from the stored
   * state authenticates with nothing. A token is what a program outside the
   * browser would use anyway.
   *
   * One sign-in for the whole file — the server allows eight per account per
   * quarter hour, and the tests below deliberately hold no session of their
   * own, which is the point of them.
   */
  const asFounder = await playwrightRequest.newContext({ baseURL });
  try {
    const signedIn = await asFounder.post("/api/auth/signin", {
      data: { username: "founder", password },
    });
    expect(signedIn.ok(), "the founder could not sign in to set the fixture up").toBeTruthy();
    const authorised = { Authorization: `Bearer ${(await signedIn.json()).token}` };

    const slug = `secret-${Date.now().toString(36)}`;
    const created = await asFounder.post("/api/rooms", {
      headers: authorised,
      data: { slug, name: "Private plans", topic: "Not for everybody", visibility: "private" },
    });
    expect(created.ok(), "the private channel could not be created").toBeTruthy();

    channel = { id: (await created.json()).id as string, slug };
    return channel;
  } finally {
    await asFounder.dispose();
  }
}

test.describe("what an outsider cannot reach", () => {
  // Each of these signs in as its own fresh account rather than the shared one.
  test.use({ storageState: { cookies: [], origins: [] } });

  test("a private channel is not in the sidebar, and its address does not open it", async ({
    page,
    baseURL,
  }) => {
    const target = await aPrivateChannel(baseURL!);
    await anOrdinaryPerson(page);

    await page.goto("/");
    await page
      .getByRole("button", { name: /search/i })
      .first()
      .waitFor();

    // Not listed. A sidebar that named it would disclose that it exists.
    await expect(page.getByRole("link", { name: /private plans/i })).toHaveCount(0);

    /*
     * And typing the address in does not open it either. This is the check
     * worth having: a client that hides a channel from a list while still
     * rendering it on request has not enforced anything.
     */
    await page.goto(`/rooms/${target.id}`);
    await expect(page.getByRole("textbox", { name: /message/i })).toHaveCount(0);
    await expect(page.getByText(/not for everybody/i)).toHaveCount(0);
  });

  test("the server refuses, whatever the client believes", async ({ page, baseURL }) => {
    const target = await aPrivateChannel(baseURL!);
    await anOrdinaryPerson(page);

    /*
     * Asked from inside the page, carrying the session the browser holds, so
     * this is the request the application itself would make rather than one
     * constructed outside it.
     *
     * Missing rather than forbidden: a 403 would confirm the channel exists,
     * which is the thing a private channel must not confirm.
     */
    const status = await page.evaluate(async (id) => {
      const response = await fetch(`/api/rooms/${id}/timeline`, { credentials: "include" });
      return response.status;
    }, target.id);

    expect(status, "an outsider could read a private channel's messages").toBe(404);
  });

  test("posting into a channel somebody is not in is refused", async ({ page, baseURL }) => {
    const target = await aPrivateChannel(baseURL!);
    await anOrdinaryPerson(page);

    const status = await page.evaluate(async (id) => {
      const response = await fetch(`/api/rooms/${id}/messages`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ body: "I should not be able to say this" }),
      });
      return response.status;
    }, target.id);

    expect([403, 404], `posting was answered with ${status}`).toContain(status);
  });

  test("an ordinary account cannot reach the administration screens", async ({ page }) => {
    await anOrdinaryPerson(page);

    /*
     * The route may render — it is a client-side application and the shell is
     * the same — but it must not show what it is for, and the server must
     * refuse the data behind it.
     */
    await page.goto("/admin");
    await expect(page.getByRole("heading", { name: /server settings/i })).toHaveCount(0);

    const status = await page.evaluate(async () => {
      const response = await fetch("/api/admin/audit?limit=1", { credentials: "include" });
      return response.status;
    });
    expect([401, 403], `the audit log answered with ${status}`).toContain(status);
  });
});
