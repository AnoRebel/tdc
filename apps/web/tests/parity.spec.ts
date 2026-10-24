import { expect, test } from "@playwright/test";

/*
 * The checklist, driven.
 *
 * The same behaviours will be walked by hand on the desktop client, so this is
 * written as a list of small independent checks rather than one long journey:
 * a failure should name the thing that failed, not the step it happened to
 * reach.
 */

const CHANNEL = "General";

/*
 * The shortcuts are bound when the shell mounts, so a key pressed before that
 * reaches nothing. Waiting for something the shell owns is the honest signal —
 * a fixed sleep would pass on a fast machine and fail on a slow one.
 */
async function shellReady(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page
    .getByRole("button", { name: /search/i })
    .first()
    .waitFor();
}

async function openChannel(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page
    .getByRole("link", { name: new RegExp(CHANNEL, "i") })
    .first()
    .click();
  await page.getByRole("textbox", { name: /message/i }).waitFor();
}

test.describe("chat parity", () => {
  test("7,9: the sidebar lists channels and one opens", async ({ page }) => {
    await page.goto("/");
    const channel = page.getByRole("link", { name: new RegExp(CHANNEL, "i") }).first();
    await expect(channel).toBeVisible();
    await channel.click();
    await expect(page.getByRole("textbox", { name: /message/i })).toBeVisible();
  });

  test("10: Ctrl K opens search", async ({ page }) => {
    await shellReady(page);
    await page.keyboard.press("Control+k");
    await expect(page.getByRole("dialog", { name: "Search" })).toBeVisible({ timeout: 5000 });
  });

  test("13: ? shows the keyboard reference", async ({ page }) => {
    await shellReady(page);
    // Not while the cursor is in a box: the unmodified shortcuts must never
    // fire at somebody who is writing.
    await page.keyboard.press("?");
    await expect(page.getByRole("dialog", { name: /keyboard|shortcut/i })).toBeVisible({
      timeout: 5000,
    });
  });

  test("14: the reference names Ctrl on this platform", async ({ page }) => {
    await shellReady(page);
    await page.keyboard.press("?");
    const reference = page.getByRole("dialog", { name: /keyboard|shortcut/i });
    await reference.waitFor();
    await expect(reference.getByText("Ctrl").first()).toBeVisible();
  });

  test("15,16: a message sends and appears", async ({ page }) => {
    await openChannel(page);
    const body = `checklist ${Date.now()}`;
    const box = page.getByRole("textbox", { name: /message/i });
    await box.fill(body);
    await box.press("Enter");
    await expect(page.getByText(body)).toBeVisible({ timeout: 10000 });
  });

  test("23: C focuses the message box", async ({ page }) => {
    await openChannel(page);
    await page.keyboard.press("Escape");
    await page.locator("body").click({ position: { x: 5, y: 5 } });
    await page.keyboard.press("c");
    await expect(page.getByRole("textbox", { name: /message/i })).toBeFocused();
  });

  test("29: the theme can be switched to dark", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Appearance" }).click();
    const menu = page.getByRole("menu", { name: "Appearance" });
    await menu.getByRole("menuitemradio", { name: "Dark" }).click();
    await expect(page.locator("html")).toHaveClass(/dark/);
  });

  test("28: the layout holds at a narrow window", async ({ page }) => {
    await page.setViewportSize({ width: 420, height: 900 });
    await page.goto("/");
    // Nothing may overflow the viewport horizontally.
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, "the page scrolls sideways at 420px").toBeLessThanOrEqual(1);
  });

  test("27: the layout holds at a wide window", async ({ page }) => {
    await page.setViewportSize({ width: 1600, height: 900 });
    await page.goto("/");
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(1);
  });
});

/*
 * The rest of the conversation: what happens to a message after it is sent,
 * and what arrives without being asked for.
 */
test.describe("a conversation in use", () => {
  async function open(page: import("@playwright/test").Page) {
    await page.goto("/");
    await page
      .getByRole("link", { name: /General/i })
      .first()
      .click();
    await page.getByRole("textbox", { name: /message/i }).waitFor();
  }

  async function send(page: import("@playwright/test").Page, body: string) {
    const box = page.getByRole("textbox", { name: /message/i });
    await box.fill(body);
    await box.press("Enter");
    await expect(page.getByText(body)).toBeVisible({ timeout: 10000 });
  }

  test("17: a message sent elsewhere arrives without a reload", async ({ page }) => {
    await open(page);

    const body = `from elsewhere ${Date.now()}`;

    /*
     * Sent from the page's own context, which holds the session — but through
     * the API rather than the interface, so nothing on screen put it there.
     * What is being checked is that the open conversation hears about a
     * message it did not send.
     */
    const status = await page.evaluate(async (text) => {
      const list = await (await fetch("/api/rooms", { credentials: "include" })).json();
      const general = (list as { id: string; slug: string }[]).find((r) => r.slug === "general");
      if (!general) return "no general channel";
      const sent = await fetch(`/api/rooms/${general.id}/messages`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ body: text }),
      });
      return sent.status;
    }, body);

    expect(status, "the message was not accepted by the server").toBe(201);
    await expect(page.getByText(body)).toBeVisible({ timeout: 15000 });
  });

  test("18: a message can be edited, and says so", async ({ page }) => {
    await open(page);
    const body = `to edit ${Date.now()}`;
    await send(page, body);

    // The controls appear on the message, so it has to be hovered first.
    await page.getByText(body).hover();
    await page.locator('button[title="Edit"]').last().click();

    /*
     * The edit box, by its name — targeting "the last textbox" found the
     * composer instead and sent a new message, which looked like an edit that
     * did not mark itself. By name rather than by element: the box became a
     * textarea when messages gained several lines, and the `input:focus` this
     * used to ask for then matched nothing. It autofocuses, so that is
     * checked too.
     */
    const editor = page.getByRole("textbox", { name: /edit this message/i });
    await expect(editor).toBeFocused();
    await editor.fill(`${body} changed`);
    await editor.press("Enter");

    await expect(page.getByText(`${body} changed`)).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(/edited/i).first()).toBeVisible();
  });

  test("19: a deleted message leaves a trace", async ({ page }) => {
    await open(page);
    const body = `to delete ${Date.now()}`;
    await send(page, body);

    await page.getByText(body).hover();
    await page.locator('button[title="Delete"]').last().click();

    // Deleting asks first, which is the point of the dialog.
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: /delete/i }).click();

    await expect(page.getByText(body)).toHaveCount(0, { timeout: 10000 });
    await expect(page.getByText(/removed/i).first()).toBeVisible();
  });

  test("21: a thread can be opened on a message", async ({ page }) => {
    await open(page);
    const body = `to thread ${Date.now()}`;
    await send(page, body);

    await page.getByText(body).hover();
    await page.locator('button[title="Reply in thread"]').last().click();

    await expect(page.getByRole("complementary").or(page.getByRole("dialog")).last()).toBeVisible({
      timeout: 8000,
    });
  });
});

/* The rest of the list, so both clients are judged by the same checks. */
test.describe("the rest of the list", () => {
  async function open(page: import("@playwright/test").Page) {
    await page.goto("/");
    await page
      .getByRole("link", { name: /General/i })
      .first()
      .click();
    await page.getByRole("textbox", { name: /message/i }).waitFor();
  }

  test("12: Alt arrows move between channels", async ({ page }) => {
    await page.goto("/");
    await page
      .getByRole("button", { name: /search/i })
      .first()
      .waitFor();
    await page.keyboard.press("Alt+ArrowDown");
    await expect(page).toHaveURL(/\/rooms\//, { timeout: 8000 });
  });

  test("20: a message can be reacted to", async ({ page }) => {
    await open(page);
    const body = `to react ${Date.now()}`;
    const box = page.getByRole("textbox", { name: /message/i });
    await box.fill(body);
    await box.press("Enter");
    await expect(page.getByText(body)).toBeVisible({ timeout: 10000 });

    await page.getByText(body).hover();
    // The quick reactions sit on the message itself.
    const react = page.locator('button[title*="React" i], button[aria-label*="React" i]').last();
    await react.click();
    await expect(page.locator('[class*="reaction" i]').first()).toBeVisible({ timeout: 8000 });
  });

  test("11: a search result opens the message it names", async ({ page }) => {
    await open(page);
    const body = `findable ${Date.now()}`;
    const box = page.getByRole("textbox", { name: /message/i });
    await box.fill(body);
    await box.press("Enter");
    await expect(page.getByText(body)).toBeVisible({ timeout: 10000 });

    await page.keyboard.press("Control+k");
    const search = page.getByRole("dialog", { name: "Search" });
    await expect(search).toBeVisible();
    await search.getByRole("textbox").first().fill(body);
    await page.waitForTimeout(1500);
    await page.keyboard.press("Enter");
    await expect(page.getByText(body).first()).toBeVisible({ timeout: 10000 });
  });

  /*
   * Its own sign-in, because signing out revokes the session — and every other
   * test in this suite reuses one. Left sharing, this test passed and took the
   * four that ran after it down with it, each reporting that it could not find
   * a control on a page it was never signed in to see.
   */
  test.describe("signing out", () => {
    test.use({ storageState: { cookies: [], origins: [] } });

    test("4: signing out returns to sign-in", async ({ page }) => {
      await page.goto("/signin");
      await page.getByLabel("Username or email").fill("founder");
      await page.getByLabel("Password").fill("correct horse battery staple");
      await page.getByRole("button", { name: "Sign in", exact: true }).click();
      await page.waitForURL((url) => !url.pathname.startsWith("/signin"));
      await page.keyboard.press("Escape");

      await page.goto("/settings");
      const out = page.getByRole("button", { name: /sign out/i }).first();
      await out.click();
      const confirm = page.getByRole("dialog").getByRole("button", { name: /sign out/i });
      if (await confirm.count()) await confirm.click();
      await expect(page).toHaveURL(/\/signin/, { timeout: 10000 });
    });
  });
});
