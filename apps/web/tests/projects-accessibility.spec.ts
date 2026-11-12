import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

import { must, project, unique } from "./projects-fixtures";

/*
 * The board and an issue's page, audited the way the conversation is.
 *
 * Each in the states worth auditing — a board with cards, filters, figures and
 * a card being planned; an issue with every field, a history, an origin and a
 * discussion — because a passing audit only says nothing was found in what was
 * rendered, and an empty board renders almost nothing.
 *
 * Chromium only, like the rest of the accessibility pass: axe reports the
 * same violations on every engine.
 */

function auditor(page: Page) {
  return new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]);
}

function describe(violations: Awaited<ReturnType<AxeBuilder["analyze"]>>["violations"]) {
  return violations
    .map((violation) => {
      const where = violation.nodes.map((node) => node.target.join(" ")).join(", ");
      return `${violation.id} (${violation.impact}): ${violation.help}\n    at ${where}`;
    })
    .join("\n  ");
}

async function settled(page: Page) {
  await expect.poll(async () => await page.title(), { timeout: 10_000 }).not.toBe("");
}

let key = "";

test.beforeAll(async ({ browser }, info) => {
  const context = await browser.newContext({ storageState: info.project.use.storageState });
  const page = await context.newPage();
  await page.goto(info.project.use.baseURL ?? "/");
  key = unique("A");
  const { state } = await project(page, key);
  const bug = await must<{ id: string }>(page, "POST", `/api/projects/${key}/labels`, {
    name: "bug",
    colour: "#dc2626",
  });
  const today = new Date().toISOString().slice(0, 10);
  const later = new Date(Date.now() + 13 * 86_400_000).toISOString().slice(0, 10);
  const cycle = await must<{ id: string }>(page, "POST", `/api/projects/${key}/cycles`, {
    name: "This fortnight",
    startsOn: today,
    endsOn: later,
  });
  const milestone = await must<{ id: string }>(page, "POST", `/api/projects/${key}/milestones`, {
    name: "Launch",
    targetDate: later,
  });
  for (const title of ["Audit the board", "Audit an issue", "Audit something finished"]) {
    await must(page, "POST", `/api/projects/${key}/issues`, {
      title,
      description: `${title}, *in full*.`,
    });
  }
  await must(page, "PUT", `/api/issues/${key}-1/labels/${bug.id}`);
  await must(page, "PUT", `/api/issues/${key}-1/assignees/founder`);
  await must(page, "PUT", `/api/issues/${key}-2/cycle`, { cycleId: cycle.id });
  await must(page, "PUT", `/api/issues/${key}-2/milestone`, { milestoneId: milestone.id });
  await must(page, "POST", `/api/issues/${key}-3/state`, { stateId: state("Done") });
  await must(page, "POST", `/api/issues/${key}-1/relations`, { kind: "blocks", issue: `${key}-2` });
  await context.close();
});

test("a board with cards, filters and figures has no accessibility violations", async ({
  page,
}) => {
  await page.goto(`/projects/${key}/board`);
  await expect(page.locator("[data-card]")).not.toHaveCount(0);
  await expect(page.locator("[data-progress] [aria-live]")).toBeVisible();
  await settled(page);
  const { violations } = await auditor(page).analyze();
  expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
});

test("a board with a card picked up and another being planned has no accessibility violations", async ({
  page,
}) => {
  await page.goto(`/projects/${key}/board`);
  await page.getByRole("combobox", { name: "Cycle", exact: true }).click();
  await page.getByRole("option", { name: "Any cycle", exact: true }).click();
  await page.getByRole("button", { name: `Plan ${key}-1` }).click();
  await expect(page.getByRole("region", { name: `Plan ${key}-1` })).toBeVisible();
  const handle = page.getByRole("button", { name: `Move ${key}-1, now in To do` });
  await handle.focus();
  await page.keyboard.press("Enter");
  await expect(handle).toHaveAttribute("aria-pressed", "true");
  await settled(page);
  const { violations } = await auditor(page).analyze();
  expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
  await page.keyboard.press("Escape");
});

test("an issue with every field filled has no accessibility violations", async ({ page }) => {
  await page.goto(`/issues/${key}-2`);
  await expect(page.getByRole("combobox", { name: "Milestone", exact: true })).toContainText(
    "Launch",
  );
  await expect(page.getByRole("region", { name: "History" })).toContainText("milestone");
  await settled(page);
  const { violations } = await auditor(page).analyze();
  expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
});

test("a project's progress, with its chart, has no accessibility violations", async ({ page }) => {
  await page.goto(`/projects/${key}/progress`);
  await expect(page.getByRole("img").first()).toBeVisible();
  await settled(page);
  const { violations } = await auditor(page).analyze();
  expect(violations, `\n  ${describe(violations)}\n`).toEqual([]);
});
