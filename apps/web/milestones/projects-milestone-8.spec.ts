import { expect, test, type Page } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * Progress: matching figures, live, and legible without a chart.
 *
 * The script made WEB with eight issues, five in Sprint 3, which is under way;
 * three issues are done, one of them in Sprint 3. Here the founder reads the
 * project's and the cycle's progress, finishes a card on the board from the
 * keyboard and watches the figures above it change without the page loading
 * again, and then reads the same progress in a browser whose charts cannot be
 * drawn at all.
 */

function figures(page: Page, within = page.locator("body")) {
  return within.locator("[data-progress]").first().locator("[aria-live]");
}

test("progress matches, follows a move without a reload, and survives a chart that cannot draw", async ({
  browser,
}) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  const broken = await browser.newContext();

  try {
    await signIn(page, "founder");

    await test.step("the project and its cycle each say how much is done", async () => {
      await page.goto("/projects/WEB/progress");
      const project = page.getByRole("region", { name: /The whole project/ });
      await expect(figures(page, project)).toContainText("3 of 8 done");
      await expect(figures(page, project)).toContainText("38% done");
      const sprint = page.locator('[data-cycle="Sprint 3"]');
      await expect(figures(page, sprint)).toContainText("1 of 5 done");
      // Drawn: an image with a description that carries the figures too.
      await expect(project.getByRole("img")).toHaveAttribute("aria-label", /now 3 of 8/);
      process.stdout.write(
        `project: ${await figures(page, project).innerText()} | Sprint 3: ${await figures(page, sprint).innerText()}\n`,
      );
    });

    await test.step("a card finished on the board changes the figures, with no reload", async () => {
      await page.goto("/projects/WEB/board");
      await expect(figures(page)).toContainText("1 of 5 done");
      // A mark on this page: a reload would take it away.
      await page.evaluate(() => {
        (window as unknown as { stayed: boolean }).stayed = true;
      });

      const handle = page.getByRole("button", { name: "Move WEB-4, now in To do" });
      await handle.focus();
      await page.keyboard.press("Enter");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("Enter");
      await expect(page.locator('[data-state="Done"] [data-card="WEB-4"]')).toBeVisible();

      await expect(figures(page)).toContainText("2 of 5 done");
      expect(await page.evaluate(() => (window as unknown as { stayed?: boolean }).stayed)).toBe(
        true,
      );
      process.stdout.write(`after moving WEB-4: ${await figures(page).innerText()}\n`);
    });

    await test.step("with charts unable to draw, the numbers are all still there", async () => {
      // No ResizeObserver: the chart adapter throws as it starts to draw.
      await broken.addInitScript(() => {
        delete (window as unknown as { ResizeObserver?: unknown }).ResizeObserver;
      });
      const plain = await broken.newPage();
      await signIn(plain, "founder");
      await plain.goto("/projects/WEB/progress");
      const project = plain.getByRole("region", { name: /The whole project/ });
      await expect(figures(plain, project)).toContainText("4 of 8 done");
      await expect(project.getByRole("img")).toHaveCount(0);
      const table = project.getByRole("table", { name: "Issues done at the end of each day" });
      await expect(table).toBeVisible();
      // Today's row says what the figures say: the chart's numbers, as words.
      const today = table.getByRole("row").last();
      await expect(today.getByRole("cell").nth(0)).toHaveText("4");
      await expect(today.getByRole("cell").nth(1)).toHaveText("8");
      process.stdout.write(
        `without a chart, today's row reads: ${(await today.innerText()).replace(/\s+/g, " ")}\n`,
      );
    });
  } finally {
    await context.close();
    await broken.close();
  }
});
