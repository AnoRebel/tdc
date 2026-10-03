import { expect, test, type Page } from "@playwright/test";

import { project, unique } from "./projects-fixtures";

/*
 * What the board and an issue's page cost to open, as a browser fetches them.
 *
 * The same idea as the initial-load budget, for two screens people open on
 * phones: what the screen itself asks the server for when somebody already in
 * the application goes to it. The board's data is also held to its budget by
 * the server's tests; this measures it where it is felt, in a browser, on a
 * project big enough for the budget to matter.
 *
 * Only the screen's own requests. Its code is the initial-load budget's
 * concern, and whether a chunk was prefetched before the click is a matter of
 * timing; the application's own polling — unread counts, presence — is the
 * same on every screen and grows with whatever the database holds. Counting
 * either made the first version of this check measure the weather. Chromium
 * only: the figure is about the application, not the engine.
 */

/** The board's JSON must fit this, whatever the project holds. */
const BOARD_DATA = 48 * 1024;

/*
 * Everything each screen asks the server for when it opens. Set above today's
 * figures with room to work in; raising one is a decision to make on purpose,
 * saying why.
 */
const SCREEN = { board: 16 * 1024, issue: 16 * 1024 };

/** A request the screen made for itself, rather than the application's polling. */
function ownRequest(entry: { url: string; type: string }): boolean {
  const path = new URL(entry.url).pathname;
  return (
    (entry.type === "fetch" || entry.type === "xhr") &&
    path.startsWith("/api/") &&
    !path.startsWith("/api/me/")
  );
}

let key = "";

test.beforeAll(async ({ browser, browserName }, info) => {
  test.skip(browserName !== "chromium", "a figure about the application, measured once");
  const context = await browser.newContext({ storageState: info.project.use.storageState });
  const page = await context.newPage();
  await page.goto(info.project.use.baseURL ?? "/");
  key = unique("P");
  await project(page, key);
  // Three hundred issues, filed from inside the page, a few at a time.
  await page.evaluate(async (key) => {
    const file = (n: number) =>
      fetch(`/api/projects/${key}/issues`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title: `Issue ${n}: a card on a board that should never ship whole`,
          description: "Long description text. ".repeat(30),
        }),
      });
    for (let n = 1; n <= 300; n += 10) {
      await Promise.all(Array.from({ length: 10 }, (_, i) => file(n + i)));
    }
  }, key);
  await context.close();
});

/** Everything fetched while doing something, by what it was. */
async function measured(page: Page, work: () => Promise<void>) {
  const fetched: { url: string; bytes: number; type: string }[] = [];
  const pending: Promise<void>[] = [];
  const listener = (response: import("@playwright/test").Response) => {
    pending.push(
      (async () => {
        // Decoded, as the budget is stated: before compression, so the figure
        // does not depend on whether a proxy in front happens to compress.
        const body = await response.body().catch(() => Buffer.alloc(0));
        fetched.push({
          url: response.url(),
          bytes: body.length,
          type: response.request().resourceType(),
        });
      })(),
    );
  };
  page.on("response", listener);
  await work();
  await page.waitForLoadState("networkidle");
  await Promise.all(pending);
  page.off("response", listener);
  // The largest few, so a figure over budget says where it went.
  for (const entry of fetched
    .filter(ownRequest)
    .sort((a, b) => b.bytes - a.bytes)
    .slice(0, 5)) {
    process.stdout.write(`  ${entry.bytes} ${entry.type} ${new URL(entry.url).pathname}\n`);
  }
  return fetched;
}

/*
 * Arrives at a screen the way somebody inside the application does: by a link
 * from the project's list, already loaded. A reload would measure the whole
 * application, which the initial-load budget already does.
 */
async function fromTheList(page: Page) {
  await page.goto(`/projects/${key}`);
  await expect(page.getByRole("link", { name: `${key}-300` })).toBeVisible();
  await page.waitForLoadState("networkidle");
}

test("the board's first load is within its budget, data and code", async ({ page }) => {
  await fromTheList(page);
  const fetched = await measured(page, async () => {
    await page
      .getByRole("navigation", { name: "Ways of looking at this project" })
      .getByRole("link", { name: "Board", exact: true })
      .click();
    await expect(page.locator('[data-state="To do"] [data-card]')).toHaveCount(20);
  });
  const board = fetched.find((entry) =>
    /\/api\/projects\/[^/]+\/board$/.test(new URL(entry.url).pathname),
  );
  expect(board, "the board was not fetched").toBeTruthy();
  const total = fetched.filter(ownRequest).reduce((sum, entry) => sum + entry.bytes, 0);
  process.stdout.write(`board: ${board!.bytes} bytes of board, ${total} bytes asked for in all\n`);
  expect(board!.bytes).toBeLessThanOrEqual(BOARD_DATA);
  expect(total).toBeLessThanOrEqual(SCREEN.board);
});

test("an issue's page is within its budget, data and code", async ({ page }) => {
  await fromTheList(page);
  const fetched = await measured(page, async () => {
    await page.getByRole("link", { name: `${key}-300` }).click();
    // Filed ten at a time, so which title became -300 is not fixed; the page is.
    await page.waitForURL(`**/issues/${key}-300`);
    await expect(page.getByRole("heading", { name: /^Issue \d+:/ })).toBeVisible();
  });
  const total = fetched.filter(ownRequest).reduce((sum, entry) => sum + entry.bytes, 0);
  process.stdout.write(`issue: ${total} bytes asked for in all\n`);
  expect(total).toBeLessThanOrEqual(SCREEN.issue);
});
