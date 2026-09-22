import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

/*
 * Whether somebody who cannot see the screen, or cannot use a mouse, can use
 * this at all.
 *
 * Run against a real browser rather than parsed markup, because the questions
 * worth asking need layout: whether a control is visible, what colour it
 * actually resolves to, where the focus ring goes. jsdom answers none of those
 * and would let all three pass while being wrong.
 */

/**
 * The rules to hold this application to.
 *
 * WCAG 2.1 A and AA, which is the level most public bodies are held to and a
 * reasonable floor for something a community relies on. Deliberately not
 * "everything axe knows": best-practice rules include opinions this project
 * has not agreed to, and a check nobody believes is a check people learn to
 * skip.
 */
function auditor(page: Page) {
  return new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]);
}

/**
 * Waits until the application has finished taking the page over.
 *
 * Nuxt renders on the client, so for a moment after load the document has no
 * title and the page is a shell. Auditing then reports a missing title that
 * is about to arrive — and hides whatever is actually wrong behind it.
 *
 * Worth saying plainly: that moment is real, and somebody using a screen
 * reader does meet an untitled page briefly on a slow connection. Fixing it
 * properly means rendering the title on the server, which this application
 * deliberately does not do. The check waits, and the gap is known.
 */
async function settled(page: Page) {
  await expect.poll(async () => await page.title(), { timeout: 10_000 }).not.toBe("");
}

/** A readable summary, so a failure says what to fix rather than how many. */
function describe(violations: Awaited<ReturnType<AxeBuilder["analyze"]>>["violations"]) {
  return violations
    .map((violation) => {
      const where = violation.nodes.map((node) => node.target.join(" ")).join(", ");
      return `${violation.id} (${violation.impact}): ${violation.help}\n    at ${where}`;
    })
    .join("\n  ");
}

test.describe("the pages somebody arrives at", () => {
  test("signing in has no accessibility violations", async ({ page }) => {
    await page.goto("/signin");

    await settled(page);

    const { violations } = await auditor(page).analyze();

    expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  });

  test("the conversation list has no accessibility violations", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: /Search/ }).waitFor();
    await settled(page);

    const { violations } = await auditor(page).analyze();

    expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  });

  test("a conversation has no accessibility violations", async ({ page }) => {
    await page.goto("/");
    await page
      .getByRole("link", { name: /General/ })
      .first()
      .click();
    await page.waitForURL(/\/rooms\//);

    await settled(page);

    const { violations } = await auditor(page).analyze();

    expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  });

  /*
   * A message offering choices, which is the newest thing a reader meets and
   * the one most likely to be a div with a click handler.
   *
   * Posted over the API rather than through the interface: the question is
   * whether the buttons are usable, not whether a composer can be driven, and
   * a fixture that failed for the second reason would look like a failure of
   * the first.
   */
  test("a message offering choices has no accessibility violations", async ({ page, request }) => {
    const signIn = await request.post("/api/auth/signin", {
      data: { username: "founder", password: "correct horse battery staple" },
    });
    expect(signIn.ok(), "could not sign in to post the fixture").toBeTruthy();
    const authorised = { Authorization: `Bearer ${(await signIn.json()).token}` };

    /*
     * A channel of its own rather than "General".
     *
     * Every other spec reads that one, and a fixture posting messages with
     * buttons into it changes what they find — which is how two keyboard
     * tests and a messaging test came to fail in the suite while passing
     * alone.
     */
    const slug = `a11y-choices-${Date.now().toString(36)}`;
    const created = await request.post("/api/rooms", {
      headers: authorised,
      data: { slug, name: slug, topic: "Messages that ask something" },
    });
    expect(created.ok(), "the fixture channel could not be created").toBeTruthy();
    const room = await created.json();

    const posted = await request.post(`/api/rooms/${room.id}/messages`, {
      headers: authorised,
      data: {
        body: `Accessible choices ${Date.now().toString(36)}`,
        actions: [
          { name: "yes", label: "Yes" },
          { name: "no", label: "No" },
          { name: "docs", label: "Read more", capability: "open", url: "https://example.test/x" },
        ],
      },
    });
    expect(posted.ok(), "the fixture message was refused").toBeTruthy();

    await page.goto("/");
    await page
      .getByRole("link", { name: new RegExp(slug, "i") })
      .first()
      .click();
    await page.waitForURL(/\/rooms\//);

    // The buttons are there, and they are buttons: a div with a click handler
    // would not be found by role, which is how a screen reader looks too.
    await expect(page.getByRole("button", { name: "Yes" }).first()).toBeVisible();

    await settled(page);

    const { violations } = await auditor(page).analyze();

    expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  });

  /*
   * A message open for editing, which the audit had never seen.
   *
   * The hint under the box — "Enter to save, Esc to cancel" — is only in the
   * document while an edit is in progress, so every previous run audited a
   * conversation without it. Its silence was not evidence that the hint was
   * readable; it was evidence that nothing had looked.
   */
  test("a message being edited has no accessibility violations", async ({ page }) => {
    const slug = `a11y-edit-${Date.now().toString(36)}`;

    await page.goto("/");
    await page
      .getByRole("button", { name: /new channel/i })
      .first()
      .click();
    await page.getByLabel(/name/i).first().fill(slug);
    await page.getByRole("button", { name: /^create$/i }).click();
    await page.getByRole("textbox", { name: /message/i }).waitFor();

    const body = `to edit ${Date.now().toString(36)}`;
    await page.getByRole("textbox", { name: /message/i }).fill(body);
    await page.keyboard.press("Enter");
    await expect(page.getByText(body, { exact: true })).toBeVisible();

    // The controls appear on the message, so it has to be hovered first.
    await page.getByText(body, { exact: true }).hover();
    await page.locator('button[title="Edit"]').last().click();
    await page.locator("input:focus").waitFor();

    await settled(page);

    const { violations } = await auditor(page).analyze();

    expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  });

  test("settings has no accessibility violations", async ({ page }) => {
    await page.goto("/settings");

    await settled(page);

    const { violations } = await auditor(page).analyze();

    expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  });
});
