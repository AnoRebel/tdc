import { expect, test, type Locator, type Page } from "@playwright/test";

import { choose, signIn } from "./helpers";

/*
 * Saved views, and one change to many issues.
 *
 * The script that runs this made a public project WEB and a private one SEC,
 * both with a "bug" label; amina is in neither. In WEB, WEB-1 and WEB-2 are
 * bugs the founder filed; WEB-4 and WEB-5 amina filed while she was a member,
 * so she may still label those two and nothing else there. SEC-1 is a bug
 * amina cannot see at all.
 *
 * The founder saves a filtered board and a search across projects, shares
 * both, and amina opens them: the board shows her the bugs, the search shows
 * her only what she may see. Then amina labels a mixed selection and reads,
 * line by line, what happened to each issue.
 */

function cards(page: Page) {
  return page.locator("[data-card]");
}

/** Chooses an option from a Select inside one part of the page, by its exact name. */
async function chooseIn(page: Page, within: Locator, label: string, option: string) {
  await within.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
  await expect(page.getByRole("listbox")).toHaveCount(0);
}

async function saveView(page: Page, name: string) {
  await page.getByRole("button", { name: "Save view" }).first().click();
  const form = page.getByRole("region", { name: "Save view" });
  await form.getByLabel("View name").fill(name);
  await form.getByLabel(/Share with everybody/).check();
  await form.getByRole("button", { name: "Save view" }).click();
  await expect(form).toHaveCount(0);
}

test("a shared view is narrowed to whoever opens it, and a mixed batch is reported item by item", async ({
  browser,
}) => {
  const founderContext = await browser.newContext();
  const aminaContext = await browser.newContext();
  const founder = await founderContext.newPage();
  const amina = await aminaContext.newPage();

  try {
    await test.step("the founder filters the board to bugs and shares it as a view", async () => {
      await signIn(founder, "founder");
      await founder.goto("/projects/WEB/board");
      await expect(cards(founder)).toHaveCount(6);
      await choose(founder, "Show issues labelled", "bug");
      await expect(cards(founder)).toHaveCount(2);
      await saveView(founder, "Bugs on the board");
    });

    await test.step("and shares a search for bugs in every project", async () => {
      await founder.goto("/issues/search?label=bug");
      await expect(founder.getByRole("region", { name: "Issues" }).locator("li")).toHaveCount(3);
      await saveView(founder, "Every bug");
      await founder.getByRole("link", { name: "Every bug" }).click();
      const found = founder.getByRole("region", { name: "Issues" }).locator("li .font-mono");
      await expect(found).toHaveCount(3);
      await expect(founder.getByText("SEC-1")).toBeVisible();
    });

    let boardView = "";
    let searchView = "";
    await test.step("amina finds both among the views shared with her", async () => {
      await signIn(amina, "amina");
      await amina.goto("/views");
      await expect(amina.getByRole("link", { name: "Every bug" })).toBeVisible();
      await amina.goto("/projects/WEB/board");
      await amina.getByRole("button", { name: "Views" }).click();
      const link = amina.getByRole("link", { name: "Bugs on the board" });
      await expect(link).toBeVisible();
      boardView = (await link.getAttribute("href")) ?? "";
      await link.click();
      await expect(amina.getByText("by founder")).toBeVisible();
      await expect(cards(amina)).toHaveCount(2);
      await expect(amina.locator('[data-card="WEB-1"]')).toBeVisible();
      await expect(amina.locator('[data-card="WEB-3"]')).toHaveCount(0);
    });

    await test.step("the search across projects shows her only what she may see", async () => {
      await amina.goto("/views");
      await amina.getByRole("link", { name: "Every bug" }).click();
      await amina.waitForURL(/\/views\/[0-9a-f-]{36}$/);
      searchView = new URL(amina.url()).pathname;
      const found = amina.getByRole("region", { name: "Issues" }).locator("li .font-mono");
      await expect(found).toHaveCount(2);
      expect((await found.allInnerTexts()).sort()).toEqual(["WEB-1", "WEB-2"]);
      await expect(amina.getByText("SEC-1")).toHaveCount(0);
      process.stdout.write(`views opened by amina: ${boardView} and ${searchView}\n`);
    });

    await test.step("amina selects four issues and adds a label to all of them", async () => {
      await amina.goto("/projects/WEB");
      for (const key of ["WEB-1", "WEB-4", "WEB-5", "WEB-6"]) {
        await amina.getByRole("checkbox", { name: `Select ${key}` }).check();
      }
      const bulk = amina.getByRole("region", { name: "Change selected issues" });
      await expect(bulk.getByText("4 selected")).toBeVisible();
      await chooseIn(amina, bulk, "Change", "Add label");
      await chooseIn(amina, bulk, "To", "triage");
      await bulk.getByRole("button", { name: "Apply to selected" }).click();

      const report = bulk.getByRole("list", { name: "What became of each issue" });
      await expect(bulk.getByText("2 changed, 2 not changed.")).toBeVisible();
      await expect(report.locator('[data-outcome="changed"]')).toHaveCount(2);
      await expect(report.locator("li").nth(0)).toContainText("WEB-1");
      await expect(report.locator("li").nth(0)).toContainText("Refused");
      await expect(report.locator("li").nth(0)).toContainText("issue.label");
      await expect(report.locator("li").nth(1)).toContainText("WEB-4");
      await expect(report.locator("li").nth(1)).toContainText("Changed");
      await expect(report.locator("li").nth(2)).toContainText("WEB-5");
      await expect(report.locator("li").nth(2)).toContainText("Changed");
      await expect(report.locator("li").nth(3)).toContainText("WEB-6");
      await expect(report.locator("li").nth(3)).toContainText("Refused");
      for (const line of await report.locator("li").allInnerTexts()) {
        process.stdout.write(`  ${line.replace(/\s+/g, " ").trim()}\n`);
      }
    });
  } finally {
    await founderContext.close();
    await aminaContext.close();
  }
});
