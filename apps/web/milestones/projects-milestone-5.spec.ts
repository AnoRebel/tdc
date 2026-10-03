import { expect, test } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * The board, on a phone-sized screen over a slow, high-latency link.
 *
 * The script that runs this filed three hundred issues. Here the board is
 * opened over a connection throttled to what a phone on a weak 3G signal gets,
 * its first load measured against the budget, one card dragged with a pointer
 * and another moved from the keyboard alone — and both moves checked to have
 * been the same request.
 */

const BUDGET = 48 * 1024;

test("a board on a slow phone: within budget, moved by dragging and by keyboard", async ({
  browser,
}) => {
  // Phone-sized, but with a mouse: the drag is a pointer drag, which is what
  // a desktop browser at this width does. The keyboard half needs no pointer.
  const context = await browser.newContext({ viewport: { width: 412, height: 915 } });
  const page = await context.newPage();

  try {
    await signIn(page, "founder");

    const cdp = await context.newCDPSession(page);
    await cdp.send("Network.enable");
    await cdp.send("Network.emulateNetworkConditions", {
      offline: false,
      latency: 300,
      downloadThroughput: (400 * 1024) / 8,
      uploadThroughput: (150 * 1024) / 8,
    });

    const moves: { url: string; body: Record<string, unknown> }[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && request.url().endsWith("/state")) {
        moves.push({ url: request.url(), body: request.postDataJSON() });
      }
    });

    let boardBytes = 0;
    await test.step("open the board and measure what it cost", async () => {
      const loaded = page.waitForResponse((response) =>
        response.url().endsWith("/api/projects/WEB/board"),
      );
      await page.goto("/projects/WEB/board");
      const response = await loaded;
      boardBytes = (await response.body()).length;
      process.stdout.write(
        `first board load: ${boardBytes} bytes for 300 issues (budget ${BUDGET})\n`,
      );
      expect(boardBytes).toBeLessThanOrEqual(BUDGET);

      await expect(page.getByRole("listitem", { name: /^To do, 300 issues$/ })).toBeVisible();
      // One page of the column, not three hundred cards.
      await expect(page.locator('[data-state="To do"] [data-card]')).toHaveCount(20);
    });

    const todo = page.locator('[data-state="To do"]');
    const doing = page.locator('[data-state="In progress"]');
    const done = page.locator('[data-state="Done"]');

    await test.step("drag a card to the next column", async () => {
      /*
       * A pointer drag as a hand makes it: press on the card, move across in
       * steps, release over the column. The column is scrolled into view
       * first, as somebody would, so the board does not scroll under the
       * pointer mid-drag.
       */
      const card = todo.locator('[data-card="WEB-300"]');
      await card.scrollIntoViewIfNeeded();
      const from = (await card.boundingBox())!;
      await page.mouse.move(from.x + from.width / 2, from.y + 20);
      await page.mouse.down();
      const to = (await doing.boundingBox())!;
      await page.mouse.move(to.x + 40, to.y + 80, { steps: 12 });
      await page.mouse.up();
      await expect(doing.locator('[data-card="WEB-300"]')).toBeVisible();
      await expect(page.getByRole("listitem", { name: /^In progress, 1 issues$/ })).toBeVisible();
      await expect.poll(() => moves.length).toBe(1);
    });

    await test.step("move another from the keyboard alone", async () => {
      const handle = page.getByRole("button", { name: "Move WEB-299, now in To do" });
      await handle.focus();
      await page.keyboard.press("Enter");
      await expect(handle).toHaveAttribute("aria-pressed", "true");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("Enter");
      await expect(done.locator('[data-card="WEB-299"]')).toBeVisible();
      await expect(page.getByText("WEB-299 moved to Done")).toBeAttached();
      await expect.poll(() => moves.length).toBe(2);
      // Focus stayed with the card it moved.
      await expect(page.getByRole("button", { name: "Move WEB-299, now in Done" })).toBeFocused();
    });

    await test.step("both moves were the same request, carrying what the card was seen in", async () => {
      expect(moves[0]!.url).toMatch(/\/api\/issues\/WEB-300\/state$/);
      expect(moves[1]!.url).toMatch(/\/api\/issues\/WEB-299\/state$/);
      for (const move of moves) {
        expect(Object.keys(move.body).sort()).toEqual(["expectedStateId", "stateId"]);
      }
      expect(moves[0]!.body.expectedStateId).toBe(moves[1]!.body.expectedStateId);
    });

    await test.step("and the server agrees after a reload", async () => {
      await page.reload();
      await expect(doing.locator('[data-card="WEB-300"]')).toBeVisible();
      await expect(done.locator('[data-card="WEB-299"]')).toBeVisible();
    });

    await page.screenshot({ path: "test-results/projects-milestone-5.png" });
  } finally {
    await context.close();
  }
});
