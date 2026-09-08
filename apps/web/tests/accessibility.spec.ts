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

  test("settings has no accessibility violations", async ({ page }) => {
    await page.goto("/settings");

    await settled(page);

    const { violations } = await auditor(page).analyze();

    expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  });
});
