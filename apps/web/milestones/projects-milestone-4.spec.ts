import { expect, test, type Page } from "@playwright/test";

import { choose, signIn } from "./helpers";

/*
 * Issues are findable the way messages are — and only by whoever may see them.
 *
 * The script that runs this filed, in a public project and a private one,
 * issues about logins: some done, some labelled bug, one changed last so it
 * ranks first. Here a member and an outsider search for them in the browser.
 */

async function search(page: Page, fields: { words?: string; state?: string; label?: string }) {
  await page.goto("/issues/search");
  const form = page.getByRole("search");
  await form.getByLabel("Words").fill(fields.words ?? "");
  await form.getByLabel("State").fill(fields.state ?? "");
  await form.getByLabel("Label").fill(fields.label ?? "");
  await form.getByRole("button", { name: "Search" }).click();
  await page.waitForURL(/\/issues\/search\?/);
}

function issueKeys(page: Page) {
  return page.getByRole("region", { name: "Issues" }).locator("li .font-mono");
}

test("search finds issues by recency, filters narrow it, and private issues stay hidden", async ({
  browser,
}) => {
  const memberContext = await browser.newContext();
  const outsiderContext = await browser.newContext();
  const member = await memberContext.newPage();
  const outsider = await outsiderContext.newPage();

  try {
    await test.step("a member finds every login issue, most recently changed first", async () => {
      await signIn(member, "founder");
      await search(member, { words: "login" });
      await expect(issueKeys(member)).toHaveText(["SEC-1", "OPEN-2", "OPEN-1"]);
    });

    await test.step("words with a state and a label narrow to what matches all three", async () => {
      await search(member, { words: "login", state: "Done", label: "bug" });
      await expect(issueKeys(member)).toHaveText(["OPEN-1"]);
    });

    await test.step("an outsider's search never shows the private project", async () => {
      await signIn(outsider, "outsider");
      await search(outsider, { words: "login" });
      await expect(issueKeys(outsider)).toHaveText(["OPEN-2", "OPEN-1"]);
      await expect(outsider.getByText("SEC-1")).toHaveCount(0);
      await expect(outsider.getByText("token in the recovery link")).toHaveCount(0);

      await search(outsider, { state: "To do" });
      await expect(outsider.getByText("SEC-1")).toHaveCount(0);
    });

    await test.step("and the project's own list searches the same way", async () => {
      await member.goto("/projects/OPEN");
      await member.getByLabel("Words").fill("login");
      await expect(issueKeys(member)).toHaveText(["OPEN-2", "OPEN-1"]);
      await choose(member, "Show issues labelled", "bug");
      await expect(issueKeys(member)).toHaveText(["OPEN-1"]);
    });
  } finally {
    await memberContext.close();
    await outsiderContext.close();
  }
});
