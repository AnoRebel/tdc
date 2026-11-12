import { expect, test, type Locator, type Page } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * Planning two cycles and a milestone, in the browser.
 *
 * The script that runs this filed WEB-1 to WEB-6 and passes the days to use,
 * worked out in Dar es Salaam: a two-week Sprint 1 ending today, a Sprint 2
 * starting tomorrow, and a milestone day already passed. Here the founder sets
 * the project's calendar, makes the cycles and the milestone, and plans issues
 * three ways — a batch from the list, one card from the board, one issue from
 * its own page.
 */

const days = {
  sprint1Start: process.env.SPRINT1_START ?? "",
  sprint1End: process.env.SPRINT1_END ?? "",
  sprint2Start: process.env.SPRINT2_START ?? "",
  sprint2End: process.env.SPRINT2_END ?? "",
  beta: process.env.BETA_DAY ?? "",
};

async function chooseIn(page: Page, within: Locator, label: string, option: string | RegExp) {
  await within.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option }).first().click();
  await expect(page.getByRole("listbox")).toHaveCount(0);
}

async function makeCycle(page: Page, name: string, first: string, last: string) {
  await page.getByRole("button", { name: "New cycle" }).click();
  const form = page.getByRole("region", { name: "New cycle" });
  await form.getByLabel("Name").fill(name);
  await form.getByLabel("First day").fill(first);
  await form.getByLabel("Last day").fill(last);
  await form.getByRole("button", { name: "Make cycle" }).click();
  await expect(form).toHaveCount(0);
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
}

test("cycles and a milestone are made, and issues planned from the list, the board and an issue", async ({
  page,
}) => {
  for (const [name, value] of Object.entries(days)) {
    expect(value, `the script passes ${name}`).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  }
  await signIn(page, "founder");

  await test.step("the project's days are Dar es Salaam's, and unfinished work carries forward", async () => {
    await page.goto("/projects/WEB/settings");
    await page.getByLabel("Time zone").fill("Africa/Dar_es_Salaam");
    await page
      .getByRole("region", { name: "Cycles and dates" })
      .getByRole("button", { name: "Save" })
      .click();
    await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
    await expect(page.getByText(/Unfinished issues move to the next cycle/)).toBeVisible();
  });

  await test.step("two cycles, back to back", async () => {
    await page.goto("/projects/WEB/cycles");
    await expect(page.getByText(/Dates are days in Africa\/Dar_es_Salaam/)).toBeVisible();
    await makeCycle(page, "Sprint 1", days.sprint1Start, days.sprint1End);
    await makeCycle(page, "Sprint 2", days.sprint2Start, days.sprint2End);
    const sprint1 = page.getByRole("listitem").filter({ hasText: "Sprint 1" });
    await expect(sprint1.locator("[data-status]")).toHaveText("Under way");
    await expect(
      page.getByRole("listitem").filter({ hasText: "Sprint 2" }).locator("[data-status]"),
    ).toHaveText("Upcoming");
  });

  await test.step("a milestone whose day has already passed", async () => {
    await page.goto("/projects/WEB/milestones");
    await page.getByRole("button", { name: "New milestone" }).click();
    const form = page.getByRole("region", { name: "New milestone" });
    await form.getByLabel("Name").fill("Beta");
    await form.getByLabel("Day to aim for").fill(days.beta);
    await form.getByRole("button", { name: "Make milestone" }).click();
    await expect(page.getByRole("link", { name: "Beta", exact: true })).toBeVisible();
  });

  await test.step("five issues into Sprint 1 at once, from the list", async () => {
    await page.goto("/projects/WEB");
    for (const key of ["WEB-1", "WEB-2", "WEB-3", "WEB-4", "WEB-5"]) {
      await page.getByRole("checkbox", { name: `Select ${key}` }).check();
    }
    const bulk = page.getByRole("region", { name: "Change selected issues" });
    await chooseIn(page, bulk, "Change", "Put in cycle");
    await chooseIn(page, bulk, "To", "Sprint 1");
    await bulk.getByRole("button", { name: "Apply to selected" }).click();
    await expect(bulk.getByText("5 changed, 0 not changed.")).toBeVisible();
  });

  await test.step("one card into Sprint 1 and Beta, from the board", async () => {
    await page.goto("/projects/WEB/board");
    // The board opens on the cycle under way; WEB-6 is in none yet.
    await expect(page.locator("[data-card]")).toHaveCount(5);
    await chooseIn(page, page.getByRole("search"), "Cycle", "Any cycle");
    await expect(page.locator("[data-card]")).toHaveCount(6);
    await page.getByRole("button", { name: "Plan WEB-6" }).click();
    const panel = page.getByRole("region", { name: "Plan WEB-6" });
    await chooseIn(page, panel, "Cycle", /^Sprint 1/);
    await chooseIn(page, panel, "Milestone", "Beta");
    await expect(panel.getByRole("combobox", { name: "Milestone", exact: true })).toHaveText(
      /Beta/,
    );
    await chooseIn(page, page.getByRole("search"), "Cycle", "Sprint 1");
    await expect(page.locator("[data-card]")).toHaveCount(6);
  });

  await test.step("and one issue into Beta, from its own page", async () => {
    await page.goto("/issues/WEB-1");
    await chooseIn(page, page.locator("body"), "Milestone", "Beta");
    await expect(page.getByRole("combobox", { name: "Milestone", exact: true })).toHaveText(/Beta/);
  });
});
