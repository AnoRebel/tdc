import { expect, test } from "@playwright/test";

import {
  account,
  fitsThePhone,
  founderWithDirectory,
  onlyInDirectoryProjects,
  people,
  secondPhone,
  signInAs,
} from "./directory.helpers";

/*
 * A developer lists skills and availability on a phone, and a client finds
 * them by those filters — and stops finding them the moment the profile is
 * hidden.
 */

onlyInDirectoryProjects();

test.describe.configure({ mode: "serial" });

const developer = people.closed;
const client = people.client;
const proposed = `Odoo ${Date.now().toString(36)}`;

test("skills and availability make a developer findable", async ({ page, request, browser }) => {
  await founderWithDirectory(request);
  const asDeveloper = await account(request, developer);
  await account(request, client);
  // Other specs give this person a profile too; this one writes it from nothing.
  await request.delete("/api/directory/me", { headers: asDeveloper });

  await signInAs(page, developer);
  await page.goto("/directory/profile");
  await page.getByLabel("Headline").fill("Flutter developer");
  await page.getByLabel("Location").fill("Zanzibar");

  // Two skills from the vocabulary, one by another spelling of it.
  const finder = page.getByPlaceholder("Find a skill");
  await finder.fill("flutter");
  await page.getByRole("button", { name: "Flutter", exact: true }).click();
  await finder.fill("golang");
  await page.getByRole("button", { name: "Go", exact: true }).click();

  // And one that is not in it yet, which waits for review.
  await finder.fill(proposed);
  await page.getByRole("button", { name: "Propose it" }).click();
  await expect(page.getByText("Waiting for review")).toBeVisible();

  // The switch draws over its own checkbox, so the click goes to what is drawn.
  // Clicked where a person would: the switch's drawn track, which is the
  // label of the checkbox underneath.
  const openToWork = page.getByRole("switch", { name: "I am open to work" });
  await openToWork.locator("xpath=..").click();
  await expect(openToWork).toBeChecked();
  await page.getByLabel("What kind of work").fill("Short contracts");
  await expect(page.getByText(/^Until /)).toBeVisible();

  await page.getByRole("button", { name: "Create profile" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Your profile is created." }),
  ).toBeVisible();
  await fitsThePhone(page);

  // The client searches, on their own phone.
  const clientsPhone = await secondPhone(browser);
  const phone = await clientsPhone.newPage();
  await signInAs(phone, client);
  await phone.goto("/directory");
  await phone.getByLabel("Skill").fill("Flutter");
  await phone.getByLabel("Location").fill("zanzibar");
  await phone.getByLabel("Available for work only").check();
  await phone.getByRole("button", { name: "Search", exact: true }).click();

  const result = phone.getByRole("link", { name: new RegExp(developer) });
  await expect(result).toBeVisible();
  await expect(result.getByText("Available for work")).toBeVisible();
  await expect(result.getByText("Go", { exact: true })).toBeVisible();
  await expect(result.getByText(proposed)).toHaveCount(0);
  await fitsThePhone(phone);

  // The proposed skill finds nobody.
  await phone.getByLabel("Skill").fill(proposed);
  await phone.getByLabel("Location").fill("");
  await phone.getByRole("button", { name: "Search", exact: true }).click();
  await expect(phone.getByText("Nobody matches")).toBeVisible();

  // Opening the profile shows what the developer chose to show.
  await phone.getByLabel("Skill").fill("go");
  await phone.getByRole("button", { name: "Search", exact: true }).click();
  await phone.getByRole("link", { name: new RegExp(developer) }).click();
  await expect(phone.getByText("Short contracts")).toBeVisible();
  await fitsThePhone(phone);
});

test("hiding a profile removes it from the same search at once", async ({ page, request }) => {
  await founderWithDirectory(request);
  const asDeveloper = await account(request, developer);
  await account(request, client);

  await signInAs(page, client);
  await page.goto("/directory?skill=flutter");
  await expect(page.getByRole("link", { name: new RegExp(developer) })).toBeVisible();

  const own = await (await request.get("/api/directory/me", { headers: asDeveloper })).json();
  const hidden = await request.put("/api/directory/me", {
    headers: asDeveloper,
    data: { ...own.profile, visibility: "hidden" },
  });
  expect(hidden.ok()).toBeTruthy();

  await page.reload();
  await expect(page.getByText("Nobody matches")).toBeVisible();
  await expect(page.getByRole("link", { name: new RegExp(developer) })).toHaveCount(0);
});
