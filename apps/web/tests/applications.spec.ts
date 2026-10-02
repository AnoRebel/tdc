import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

/*
 * Applications, administered through the interface alone: registered, approved
 * having been shown exactly what it will hold and what it sends elsewhere,
 * restricted to a channel, given a second credential and its first revoked,
 * and removed — deciding what happens to what it posted.
 *
 * The only API calls here are the application's own, to show that the
 * credential the page handed over works, and works only where it should.
 *
 * Chromium only: what is under test is the server and the page's wiring, not
 * the engine.
 */

const password = "correct horse battery staple";

test.beforeEach(({ browserName }) => {
  test.skip(browserName !== "chromium", "the wiring, not the engine, is under test");
});

test("an administrator manages an application from start to finish", async ({ page, request }) => {
  const signedIn = await request.post("/api/auth/signin", {
    data: { username: "founder", password },
  });
  const auth = { Authorization: `Bearer ${(await signedIn.json()).token}` };
  const suffix = Date.now().toString(36);
  const allowed = await (
    await request.post("/api/rooms", {
      headers: auth,
      data: { slug: `deploys-${suffix}`, name: `deploys-${suffix}` },
    })
  ).json();
  const elsewhere = await (
    await request.post("/api/rooms", {
      headers: auth,
      data: { slug: `random-${suffix}`, name: `random-${suffix}` },
    })
  ).json();
  const name = `Deploy bot ${suffix}`;

  // Registered through the form, disclosing what it sends elsewhere.
  await page.goto("/applications");
  const register = page.getByRole("form", { name: "Register an application" });
  await register.getByRole("textbox", { name: "Name" }).fill(name);
  await register.getByRole("textbox", { name: "What it is for" }).fill("Posts deployment results.");
  await register.getByText("channel.post", { exact: true }).click();
  await register.getByText("It sends data to a service outside this server").click();
  await register
    .getByRole("textbox", { name: "What it sends, and where" })
    .fill("Build numbers, to ci.example.org");
  await register.getByRole("button", { name: "Register", exact: true }).click();

  const card = page.locator(`[data-application="${name}"]`);
  await expect(card).toContainText("Awaiting approval");

  // Approval shows exactly what it will hold, and what leaves the server.
  await card.getByRole("button", { name: "Review and approve" }).click();
  const dialog = page.getByRole("dialog", { name: `Approve ${name}` });
  await expect(dialog.locator("[data-approval-permissions] li")).toHaveCount(1);
  await expect(dialog.locator("[data-approval-permissions]")).toContainText("channel.post");
  await expect(dialog.locator("[data-approval-disclosure]")).toContainText(
    "Build numbers, to ci.example.org",
  );
  const audit = await new AxeBuilder({ page })
    .include('[role="dialog"]')
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(audit.violations.map((violation) => `${violation.id}: ${violation.help}`)).toEqual([]);

  await dialog.getByText(`#${allowed.name}`, { exact: true }).click();
  await dialog.getByRole("button", { name: "Approve with these 1 permissions" }).click();

  const notice = page.getByRole("status").filter({ hasText: "not shown again" });
  await expect(notice).toBeVisible();
  const first = (await notice.locator("code").textContent())?.trim() ?? "";
  await expect(card).toContainText("Approved");

  // The credential works in its channel and nowhere else.
  const asApp = (credential: string) => ({ Authorization: `Bearer ${credential}` });
  expect(
    (
      await request.post(`/api/rooms/${allowed.id}/messages`, {
        headers: asApp(first),
        data: { body: "Deployed build 41." },
      })
    ).status(),
  ).toBe(201);
  expect(
    (
      await request.post(`/api/rooms/${elsewhere.id}/messages`, {
        headers: asApp(first),
        data: { body: "not here" },
      })
    ).status(),
  ).toBe(403);

  // A second credential, then the first revoked — through the page.
  await page.getByRole("button", { name: "Done" }).click();
  await card.getByRole("button", { name: "Channels and credentials" }).click();
  await card.getByRole("button", { name: "Issue another credential" }).click();
  const second = (await notice.locator("code").textContent())?.trim() ?? "";
  expect(second).not.toBe(first);
  await page.getByRole("button", { name: "Done" }).click();
  await card.getByRole("button", { name: "Revoke" }).last().click();
  await expect(card.getByRole("button", { name: "Revoke" })).toHaveCount(1);
  const statuses = await Promise.all(
    [first, second].map(async (credential) =>
      (
        await request.post(`/api/rooms/${allowed.id}/messages`, {
          headers: asApp(credential),
          data: { body: "which works?" },
        })
      ).status(),
    ),
  );
  expect(statuses.sort()).toEqual([201, 401]);

  // Removed, keeping what it posted.
  await card.getByRole("button", { name: `Remove ${name}` }).click();
  const removal = page.getByRole("dialog", { name: `Remove ${name}` });
  await removal.getByText("Keep its messages").click();
  await removal.getByRole("button", { name: "Remove application" }).click();
  await expect(card).toHaveCount(0);
  expect(
    (
      await request.post(`/api/rooms/${allowed.id}/messages`, {
        headers: asApp(second),
        data: { body: "after removal" },
      })
    ).status(),
  ).toBe(401);
  const timeline = await (
    await request.get(`/api/rooms/${allowed.id}/timeline`, { headers: auth })
  ).text();
  expect(timeline).toContain("Deployed build 41.");
});
