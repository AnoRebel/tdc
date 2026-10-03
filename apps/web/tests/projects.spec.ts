import { expect, test, type BrowserContext, type Page } from "@playwright/test";

import { api, must, project, secondPerson, unique } from "./projects-fixtures";

/*
 * Projects, end to end, on every engine a release is gated on.
 *
 * The conversation link in both directions and what it withholds; two people
 * moving one card; and a batch that is only partly allowed. The founder is the
 * stored session; a second person signs in once for the file.
 */

test.describe.configure({ mode: "serial" });

let outsider: { context: BrowserContext; page: Page };

test.beforeAll(async ({ browser }, info) => {
  // A sign-in through the page, and the introduction waited for: slower on
  // some engines than the default allowance for a hook.
  test.setTimeout(90_000);
  const baseURL = info.project.use.baseURL ?? "http://127.0.0.1:8099";
  outsider = await secondPerson(browser, baseURL, "e2e-outsider");
});

test.afterAll(async () => {
  await outsider?.context.close();
});

async function choose(page: Page, label: string, option: string | RegExp) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option }).first().click();
  await expect(page.getByRole("listbox")).toHaveCount(0);
}

test("a message becomes an issue that shows where it came from, and withholds it from outsiders", async ({
  page,
}) => {
  await page.goto("/");
  const key = unique("L");
  await project(page, key);
  const slug = `private-${key.toLowerCase()}`;
  const room = await must<{ id: string }>(page, "POST", "/api/rooms", {
    slug,
    name: slug,
    topic: "Where something was decided",
    visibility: "private",
  });
  const said = `The export drops the last row when the file ends without a newline ${key}`;
  await must(page, "POST", `/api/rooms/${room.id}/messages`, { body: said });

  await test.step("the message is turned into an issue from its menu", async () => {
    await page.goto(`/rooms/${room.id}`);
    await page.getByText(said).click({ button: "right" });
    await page.getByRole("menuitem", { name: "Create an issue from this" }).click();
    const dialog = page.getByRole("dialog", { name: "Create an issue from this message" });
    await choose(page, "Project", new RegExp(`^${key} ·`));
    await expect(dialog.getByLabel("Title")).toHaveValue(said);
    await dialog.getByRole("button", { name: "New issue" }).click();
    await page.waitForURL(`**/issues/${key}-1`);
  });

  await test.step("the issue shows the message to somebody who can read it", async () => {
    const origin = page.getByRole("region", { name: "Where this came from" });
    await expect(origin).toContainText(said);
    await expect(origin.getByRole("link", { name: `Open it in ${slug}` })).toBeVisible();
  });

  await test.step("and the message shows the issue", async () => {
    await page.goto(`/rooms/${room.id}`);
    await expect(page.getByRole("link", { name: `Tracked as ${key}-1: ${said}` })).toBeVisible();
  });

  await test.step("somebody outside the channel sees that there was one, and nothing of it", async () => {
    await outsider.page.goto(`/issues/${key}-1`);
    const origin = outsider.page.getByRole("region", { name: "Where this came from" });
    await expect(origin).toContainText(
      "This issue was made from a message in a conversation you cannot see.",
    );
    await expect(origin).not.toContainText(slug);
    await expect(origin).not.toContainText("founder");
    await expect(origin.getByRole("link")).toHaveCount(0);
    // Nor can they ask the room which issues came from it.
    const asked = await api(outsider.page, "GET", `/api/rooms/${room.id}/issues`);
    expect(asked.status).toBe(404);
  });
});

test("two people moving one card end up agreeing where it is", async ({ page, browser }, info) => {
  await page.goto("/");
  const key = unique("M");
  const { state } = await project(page, key);
  await must(page, "POST", `/api/projects/${key}/issues`, { title: "One card, two hands" });

  const second = await browser.newContext({ storageState: info.project.use.storageState });
  const other = await second.newPage();
  try {
    await page.goto(`/projects/${key}/board`);
    await other.goto(`/projects/${key}/board`);
    const card = `[data-card="${key}-1"]`;
    await expect(page.locator(`[data-state="To do"] ${card}`)).toBeVisible();
    await expect(other.locator(`[data-state="To do"] ${card}`)).toBeVisible();

    await test.step("one moves it to In progress", async () => {
      const handle = page.getByRole("button", { name: `Move ${key}-1, now in To do` });
      await handle.focus();
      await page.keyboard.press("Enter");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("Enter");
      await expect(page.locator(`[data-state="In progress"] ${card}`)).toBeVisible();
    });

    await test.step("the other, still seeing it in To do, moves it to Done and is told", async () => {
      const handle = other.getByRole("button", { name: `Move ${key}-1, now in To do` });
      await handle.focus();
      await other.keyboard.press("Enter");
      await other.keyboard.press("ArrowRight");
      await other.keyboard.press("ArrowRight");
      await other.keyboard.press("Enter");
      await expect(other.getByRole("alert")).toContainText(`Somebody else moved ${key}-1 first`);
      // Its board now shows where the card really is.
      await expect(other.locator(`[data-state="In progress"] ${card}`)).toBeVisible();
    });

    await test.step("and the issue moved once", async () => {
      const issue = await must<{ state: { id: string } }>(page, "GET", `/api/issues/${key}-1`);
      expect(issue.state.id).toBe(state("In progress"));
      const history = await must<{ type: string }[]>(page, "GET", `/api/issues/${key}-1/history`);
      expect(history.filter((entry) => entry.type === "issue.state_changed")).toHaveLength(1);
    });
  } finally {
    await second.close();
  }
});

test("a batch that is only partly allowed changes what it may and says what it refused", async ({
  page,
}) => {
  await page.goto("/");
  const key = unique("B");
  await project(page, key);
  const label = await must<{ id: string }>(page, "POST", `/api/projects/${key}/labels`, {
    name: "triage",
  });
  await must(page, "POST", `/api/projects/${key}/issues`, { title: "Filed by the founder" });

  // The outsider files one while a member, and then is not one.
  await must(page, "PUT", `/api/projects/${key}/members/e2e-outsider`, { role: "member" });
  await outsider.page.goto("/");
  await must(outsider.page, "POST", `/api/projects/${key}/issues`, {
    title: "Filed by the outsider",
  });
  await must(page, "DELETE", `/api/projects/${key}/members/e2e-outsider`);

  const you = outsider.page;
  await you.goto(`/projects/${key}`);
  for (const ref of [`${key}-1`, `${key}-2`]) {
    await you.getByRole("checkbox", { name: `Select ${ref}` }).check();
  }
  const bulk = you.getByRole("region", { name: "Change selected issues" });
  await bulk.getByRole("combobox", { name: "Change", exact: true }).click();
  await you.getByRole("option", { name: "Add label", exact: true }).click();
  await expect(you.getByRole("listbox")).toHaveCount(0);
  await bulk.getByRole("button", { name: "Apply to selected" }).click();

  const report = bulk.getByRole("list", { name: "What became of each issue" });
  await expect(bulk.getByText("1 changed, 1 not changed.")).toBeVisible();
  await expect(report.locator('[data-outcome="refused"]')).toContainText(`${key}-1`);
  await expect(report.locator('[data-outcome="refused"]')).toContainText("issue.label");
  await expect(report.locator('[data-outcome="changed"]')).toContainText(`${key}-2`);

  // Each change in its own issue's history, and nothing on the refused one.
  const changed = await must<{ type: string }[]>(page, "GET", `/api/issues/${key}-2/history`);
  expect(changed.map((entry) => entry.type)).toContain("issue.labeled");
  const refused = await must<{ type: string }[]>(page, "GET", `/api/issues/${key}-1/history`);
  expect(refused.map((entry) => entry.type)).not.toContain("issue.labeled");
  expect(label.id).toBeTruthy();
});
