import { expect, test } from "@playwright/test";

import { choose, openTab, signIn } from "./helpers";

/*
 * Projects exist and issues can be filed — entirely in the browser.
 *
 * A project is started, its workflow configured to three states of its own, an
 * issue filed and moved through all three, and its history read back in order.
 * Nothing here calls the API directly: every step is somebody using the pages.
 */
test("a project is configured, and an issue moves through it with its history intact", async ({
  page,
}) => {
  await signIn(page, "founder");

  await test.step("start a project", async () => {
    await page.goto("/projects");
    await page.getByRole("button", { name: "New project" }).click();
    await page.getByLabel("Name").fill("Community website");
    await page.getByLabel("Key").fill("WEB");
    await page.getByRole("button", { name: "Create" }).click();
    await page.waitForURL("**/projects/WEB");
    await expect(page.getByRole("heading", { name: "Community website" })).toBeVisible();
  });

  await test.step("configure three states: Triage, Review, Done", async () => {
    await openTab(page, "Settings");
    await page.waitForURL("**/projects/WEB/settings");

    await page.getByRole("button", { name: "Rename To do" }).click();
    await page.getByLabel("State name").fill("Triage");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("button", { name: "Rename Triage" })).toBeVisible();

    await page.getByLabel("New state").fill("Review");
    await page.getByRole("button", { name: "Add state" }).click();
    await expect(page.getByRole("button", { name: "Rename Review" })).toBeVisible();

    await page.getByRole("button", { name: "Remove In progress" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Remove" }).click();
    await expect(page.getByRole("button", { name: "Rename In progress" })).toHaveCount(0);

    // Review was added last; it belongs before Done.
    await page.getByRole("button", { name: "Move Review earlier" }).click();
    const states = page.locator("#states-heading ~ ol > li");
    await expect(states).toHaveCount(3);
    await expect(states.nth(0)).toContainText("Triage");
    await expect(states.nth(1)).toContainText("Review");
    await expect(states.nth(2)).toContainText("Done");
  });

  await test.step("file an issue", async () => {
    await openTab(page, "Issues");
    await page.getByRole("button", { name: "New issue" }).click();
    await page.getByLabel("Title").fill("Search results ignore diacritics");
    await page.getByLabel("Description").fill("Searching for “Dar” misses “Dár”.");
    await page.getByRole("button", { name: "New issue" }).click();
    await page.waitForURL("**/issues/WEB-1");
    await expect(
      page.getByRole("heading", { name: "Search results ignore diacritics" }),
    ).toBeVisible();
  });

  const history = page.getByRole("region", { name: "History" }).locator("li");

  await test.step("move it through all three states", async () => {
    await expect(history).toHaveCount(1);
    await expect(page.getByRole("combobox", { name: "State" })).toContainText("Triage");

    await choose(page, "State", "Review");
    await expect(history).toHaveCount(2);
    await expect(page.getByRole("combobox", { name: "State" })).toContainText("Review");

    await choose(page, "State", "Done");
    await expect(history).toHaveCount(3);
    await expect(page.getByRole("combobox", { name: "State" })).toContainText("Done");
  });

  await test.step("read its complete history, in order", async () => {
    await expect(history.nth(0)).toContainText("founder filed this");
    await expect(history.nth(1)).toContainText("founder moved this from Triage to Review");
    await expect(history.nth(2)).toContainText("founder moved this from Review to Done");

    // And it survives a reload: this is the server's record, not the page's.
    await page.reload();
    await expect(history).toHaveCount(3);
    await expect(history.nth(2)).toContainText("from Review to Done");
  });

  await page.screenshot({ path: "test-results/projects-milestone-1.png", fullPage: true });
});
