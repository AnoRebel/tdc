import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";

/*
 * Interactive messages beyond buttons, in the web client.
 *
 * A program posts a message offering a menu and a field; a person chooses from
 * the menu and types into the field in a real browser; the program answers one
 * of them privately and then replaces the message's controls for everybody —
 * and a press through the replaced view is refused.
 *
 * Two people, in two browsers, because "only the presser sees it" and "every
 * viewer sees the update" are both claims about somebody other than the person
 * pressing.
 *
 * Run by scripts/demo-milestone-integrations-1.sh against a fresh server, and
 * skipped otherwise: it registers accounts, which the suite's shared server
 * limits per hour.
 */

test.skip(!process.env.TDC_DEMO_INTEGRATIONS, "run by scripts/demo-milestone-integrations-1.sh");

const password = "correct horse battery staple";

async function tokenFor(request: APIRequestContext, username: string): Promise<string> {
  const signedIn = await request.post("/api/auth/signin", { data: { username, password } });
  expect(signedIn.ok(), `could not sign in as ${username}`).toBeTruthy();
  return (await signedIn.json()).token;
}

async function register(request: APIRequestContext, username: string): Promise<string> {
  const registered = await request.post("/api/auth/register", { data: { username, password } });
  expect(registered.ok(), `could not register ${username}`).toBeTruthy();
  return tokenFor(request, username);
}

/** A second person's browser, signed in through the page like anybody would. */
async function signedInPage(browser: Browser, username: string): Promise<Page> {
  const context = await browser.newContext({ storageState: undefined });
  const page = await context.newPage();
  await page.goto("/signin");
  await page.getByLabel("Username or email").fill(username);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));

  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 5000 }).catch(() => {});
  if (await tour.count()) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }
  return page;
}

async function openChannel(page: Page, slug: string) {
  await page.goto("/");
  await page
    .getByRole("link", { name: new RegExp(slug, "i") })
    .first()
    .click();
  await page.getByRole("textbox", { name: /message/i }).waitFor();
}

test("menus, fields, private answers and updates in place", async ({ page, request, browser }) => {
  const founder = { Authorization: `Bearer ${await tokenFor(request, "founder")}` };
  const program = { Authorization: `Bearer ${await register(request, "deploybot")}` };
  const amina = { Authorization: `Bearer ${await register(request, "amina")}` };

  const slug = `deploys-${Date.now().toString(36)}`;
  const created = await request.post("/api/rooms", {
    headers: founder,
    data: { slug, name: slug, visibility: "public" },
  });
  expect(created.ok()).toBeTruthy();
  const room = await created.json();
  for (const who of [program, amina]) {
    expect((await request.post(`/api/rooms/${room.id}/join`, { headers: who })).ok()).toBeTruthy();
  }

  // The program asks, with a menu and a field beside a button.
  const asked = await request.post(`/api/rooms/${room.id}/messages`, {
    headers: program,
    data: {
      body: "Build 41 is ready. Where should it go?",
      clientToken: crypto.randomUUID(),
      actions: [
        { name: "hold", label: "Not yet" },
        {
          name: "environment",
          label: "Environment",
          kind: "select",
          options: [
            { value: "staging", label: "Staging" },
            { value: "production", label: "Production" },
          ],
        },
        { name: "note", label: "Release note", kind: "input", placeholder: "What changed?" },
      ],
    },
  });
  expect(asked.ok(), await asked.text()).toBeTruthy();
  const question = await asked.json();

  const other = await signedInPage(browser, "amina");
  await openChannel(page, slug);
  await openChannel(other, slug);

  /*
   * The menu is a list of choices and the field a labelled text box — read
   * from the accessibility tree, which is what a screen reader announces
   * from, rather than from the markup's appearance.
   */
  const menu = page.getByRole("combobox", { name: "Environment" });
  await expect(menu).toBeVisible();
  const field = page.getByRole("textbox", { name: "Release note" });
  await expect(field).toBeVisible();

  const audit = await new AxeBuilder({ page })
    .include(`[data-message-id="${question.id}"]`)
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(
    audit.violations.map((violation) => `${violation.id}: ${violation.help}`),
    "the message's controls have accessibility violations",
  ).toEqual([]);

  await menu.click();
  const listbox = page.getByRole("listbox");
  await expect(listbox).toBeVisible();
  await expect(listbox.getByRole("option")).toHaveText(["Staging", "Production"]);
  await listbox.getByRole("option", { name: "Staging" }).click();

  await field.fill("Fixes the login loop");
  await page.getByRole("button", { name: "Send Release note" }).click();

  // Both answers reach the program, carrying what was chosen and typed.
  let pressID = "";
  await expect(async () => {
    const timeline = await (
      await request.get(`/api/rooms/${room.id}/timeline`, { headers: program })
    ).json();
    const presses = timeline.filter(
      (event: { type: string; content?: { pressed?: string } }) =>
        event.type === "action.pressed" && event.content?.pressed === question.id,
    );
    const values = presses.map((press: { content: { value: string } }) => press.content.value);
    expect(values).toEqual(expect.arrayContaining(["staging", "Fixes the login loop"]));
    pressID = presses.find(
      (press: { content: { action: string } }) => press.content.action === "environment",
    ).id;
  }).toPass({ timeout: 15_000 });

  // The program answers the founder alone.
  const replied = await request.post(
    `/api/rooms/${room.id}/presses/${encodeURIComponent(pressID)}/reply`,
    { headers: program, data: { body: "Staging is locked until 17:00." } },
  );
  expect(replied.ok(), await replied.text()).toBeTruthy();
  await expect(page.getByText("Staging is locked until 17:00.")).toBeVisible();
  await expect(page.getByText("Only you can see this")).toBeVisible();

  /*
   * And the other person does not see it. Checked after something they do
   * see arrives, so their page has demonstrably caught up past the moment
   * the private answer was sent.
   */
  await request.post(`/api/rooms/${room.id}/messages`, {
    headers: program,
    data: { body: "Checking the build queue.", clientToken: crypto.randomUUID() },
  });
  await expect(other.getByText("Checking the build queue.")).toBeVisible();
  await expect(other.getByText("Staging is locked until 17:00.")).toHaveCount(0);

  // The program replaces the question for everybody.
  const updated = await request.put(
    `/api/rooms/${room.id}/messages/${encodeURIComponent(question.id)}/controls`,
    {
      headers: program,
      data: {
        version: 0,
        body: "Build 41 is going to staging.",
        actions: [{ name: "rollback", label: "Roll back" }],
      },
    },
  );
  expect(updated.ok(), await updated.text()).toBeTruthy();

  for (const viewer of [page, other]) {
    await expect(viewer.getByText("Build 41 is going to staging.")).toBeVisible();
    await expect(viewer.getByRole("button", { name: "Roll back" })).toBeVisible();
    await expect(viewer.getByRole("combobox", { name: "Environment" })).toHaveCount(0);
  }

  // A second activation through the replaced view is refused as stale.
  const stale = await request.post(
    `/api/rooms/${room.id}/messages/${encodeURIComponent(question.id)}/actions/environment`,
    { headers: amina, data: { value: "production", version: 0 } },
  );
  expect(stale.status(), "a press through the replaced view was not refused").toBe(409);

  // While the new control, pressed in the browser, is accepted.
  await other.getByRole("button", { name: "Roll back" }).click();
  await expect(async () => {
    const timeline = await (
      await request.get(`/api/rooms/${room.id}/timeline`, { headers: program })
    ).json();
    const rollback = timeline.find(
      (event: { type: string; content?: { action?: string; version?: number } }) =>
        event.type === "action.pressed" && event.content?.action === "rollback",
    );
    expect(rollback?.content.version).toBe(1);
  }).toPass({ timeout: 15_000 });

  // eslint-disable-next-line no-console
  console.log(`MILESTONE_ROOM=${room.id}`);
  await other.context().close();
});
