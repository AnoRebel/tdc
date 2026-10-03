import { expect, test, type Page } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * A newcomer, with nothing but the guide.
 *
 * Each step below is a step of "From a conversation to done" in
 * docs/projects.md, taken by somebody who registered a minute ago, using only
 * what that step names on the screen. The conversation is live: the founder
 * says something in General while the newcomer is reading it, and that message
 * becomes the issue.
 */

const SAID = "The sign-up form forgets what you typed when the password is too short";

async function choose(page: Page, label: string, option: string | RegExp) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option }).first().click();
  await expect(page.getByRole("listbox")).toHaveCount(0);
}

test("a newcomer follows the guide from a live conversation to a counted milestone", async ({
  browser,
}) => {
  const newcomerContext = await browser.newContext();
  const founderContext = await browser.newContext();
  const page = await newcomerContext.newPage();
  const founder = await founderContext.newPage();

  try {
    await signIn(page, "newcomer");
    await signIn(founder, "founder");

    await test.step("1. start a project", async () => {
      await page.getByRole("link", { name: "Projects", exact: true }).click();
      await page.getByRole("button", { name: "New project" }).click();
      await page.getByLabel("Name").fill("Community website");
      await page.getByLabel("Key").fill("WEB");
      await page.getByRole("button", { name: "Create" }).click();
      await page.waitForURL("**/projects/WEB");
    });

    await test.step("2. turn a message, said just now, into an issue", async () => {
      await page.goto("/");
      await page.getByRole("navigation").getByRole("link", { name: "General" }).first().click();
      await page.waitForURL(/\/rooms\//);
      const join = page.getByRole("button", { name: "Join", exact: true });
      if (await join.count()) await join.click();

      await founder.goto("/");
      await founder.getByRole("navigation").getByRole("link", { name: "General" }).first().click();
      const composer = founder.getByRole("textbox", { name: /message/i });
      await composer.fill(SAID);
      await founder.keyboard.press("Enter");

      // Live, without reloading.
      await expect(page.getByText(SAID)).toBeVisible({ timeout: 20_000 });
      await page.getByText(SAID).click({ button: "right" });
      await page.getByRole("menuitem", { name: "Create an issue from this" }).click();
      const dialog = page.getByRole("dialog", { name: "Create an issue from this message" });
      await choose(page, "Project", /^WEB ·/);
      await expect(dialog.getByLabel("Title")).toHaveValue(SAID);
      await dialog.getByRole("button", { name: "New issue" }).click();
      await page.waitForURL("**/issues/WEB-1");
      await expect(page.getByRole("region", { name: "Where this came from" })).toContainText(SAID);

      await page.goBack();
      await expect(page.getByRole("link", { name: `Tracked as WEB-1: ${SAID}` })).toBeVisible();
      await page.goto("/issues/WEB-1");
    });

    await test.step("3. talk about it", async () => {
      // Whoever filed it is already in its discussion, as the guide says:
      // there is no Join for them, only the message box.
      const discussion = page.getByRole("region", { name: "Discussion" });
      await expect(discussion.getByRole("button", { name: "Join" })).toHaveCount(0);
      await discussion.getByRole("textbox", { name: /message/i }).fill("@founder taking this one.");
      await page.keyboard.press("Enter");
      await expect(discussion.getByText("taking this one.")).toBeVisible();
    });

    await test.step("4. give it somewhere to land", async () => {
      await page.goto("/projects/WEB");
      await page
        .getByRole("navigation", { name: "Ways of looking at this project" })
        .getByRole("link", { name: "Milestones", exact: true })
        .click();
      await page.getByRole("button", { name: "New milestone" }).click();
      const form = page.getByRole("region", { name: "New milestone" });
      await form.getByLabel("Name").fill("Launch");
      await form.getByRole("button", { name: "Make milestone" }).click();
      await expect(page.getByRole("link", { name: "Launch", exact: true })).toBeVisible();

      await page.goto("/issues/WEB-1");
      await choose(page, "Milestone", "Launch");
      await expect(page.getByRole("combobox", { name: "Milestone", exact: true })).toHaveText(
        /Launch/,
      );
    });

    await test.step("5. move it across the board, from the keyboard", async () => {
      await page.goto("/projects/WEB");
      await page
        .getByRole("navigation", { name: "Ways of looking at this project" })
        .getByRole("link", { name: "Board", exact: true })
        .click();
      const handle = page.getByRole("button", { name: "Move WEB-1, now in To do" });
      await handle.focus();
      await page.keyboard.press("Enter");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("Enter");
      await expect(page.locator('[data-state="Done"] [data-card="WEB-1"]')).toBeVisible();
    });

    await test.step("6. see it counted", async () => {
      await page
        .getByRole("navigation", { name: "Ways of looking at this project" })
        .getByRole("link", { name: "Milestones", exact: true })
        .click();
      await page.getByRole("link", { name: "Launch", exact: true }).click();
      await expect(page.locator("[data-progress]")).toContainText("1 of 1 done");

      await page
        .getByRole("navigation", { name: "Ways of looking at this project" })
        .getByRole("link", { name: "Progress", exact: true })
        .click();
      await expect(page.locator("[data-progress]").first()).toContainText("1 of 1 done");

      await page.goto("/issues/WEB-1");
      const history = page.getByRole("region", { name: "History" });
      await expect(history).toContainText("newcomer filed this");
      await expect(history).toContainText("newcomer put this in the milestone Launch");
      await expect(history).toContainText("newcomer moved this from To do to Done");
    });
  } finally {
    await newcomerContext.close();
    await founderContext.close();
  }
});
