import { readFile } from "node:fs/promises";

import { expect, test } from "@playwright/test";
import type { APIRequestContext, Page } from "@playwright/test";

import {
  account,
  fitsThePhone,
  founderWithDirectory,
  existing,
  noEnquiryPending,
  onlyInDirectoryProjects,
  password,
  people,
  secondPhone,
  signInAs,
  placeIn,
} from "./directory.helpers";

/*
 * The whole directory, as the user guide describes it, by two people on two
 * phones at once and through nothing but the interface: each joins the server,
 * the developer makes a profile anyone can find, the client finds it by skill
 * and place and sends an enquiry, the developer takes it up and proposes, both
 * accept, and both export the same agreement. Then the work: the developer
 * tracks it in a private project, starts it and says it is done, and the
 * client confirms — each phone following the other's changes as they happen —
 * while a colleague in that project learns nothing of the engagement.
 *
 * The other directory specs set their scenes over the API so that each checks
 * one thing; this one sets up nothing but the project, because what it checks
 * is that the steps a person would take connect.
 */

onlyInDirectoryProjects();
test.describe.configure({ mode: "serial" });

/** Joins through the registration form, or signs in if an earlier spec already did. */
async function join(page: Page, request: APIRequestContext, username: string) {
  if (await existing(request, username)) {
    await signInAs(page, username);
    return;
  }
  await page.goto("/register");
  await page.getByRole("textbox", { name: "Username" }).fill(username);
  await page.getByLabel(/^password/i).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/register"));
  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 4000 }).catch(() => {});
  if (await tour.count()) await page.keyboard.press("Escape");
}

test("two people go from a profile to finished, tracked work through the interface", async ({
  page,
  request,
  browser,
}) => {
  // The whole journey, twice the length of any other directory check.
  test.setTimeout(240_000);
  await founderWithDirectory(request);
  const subject = `Mobile money checkout ${Date.now().toString(36)}`;
  const place = `Dodoma${Date.now().toString(36)}`;

  // The developer joins and writes a profile anyone can find.
  await join(page, request, people.developer);
  const asDeveloper = await existing(request, people.developer);
  if (!asDeveloper) throw new Error("the developer could not sign in after joining");
  // Earlier specs may have given this person a profile; the guide starts without one.
  await request.delete("/api/directory/me", { headers: asDeveloper });
  await placeIn(request, asDeveloper, place);
  await page.goto("/directory/profile");
  await page.getByLabel("Headline").fill("Payments developer");
  const finder = page.getByPlaceholder("Find a skill");
  await finder.fill("golang");
  await page.getByRole("button", { name: "Go", exact: true }).click();
  for (const group of ["Who can find you", "Who sees what you wrote"]) {
    await page.getByRole("group", { name: group }).getByText("Anyone", { exact: true }).click();
  }
  await page.getByRole("button", { name: "Create profile" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Your profile is created." }),
  ).toBeVisible();
  await fitsThePhone(page);

  // The client joins on their own phone and finds the developer by skill and place.
  const clientsPhone = await secondPhone(browser);
  const client = await clientsPhone.newPage();
  await join(client, request, people.client);
  // An enquiry an earlier spec left unanswered would stand in the way of this one.
  await noEnquiryPending(request);
  await client.goto("/directory");
  await client.getByLabel("Skill").fill("Go");
  await client.getByLabel("Location").fill(place.toLowerCase());
  await client.getByRole("button", { name: "Search", exact: true }).click();
  await client.getByRole("link", { name: new RegExp(people.developer) }).click();
  await expect(client.getByText("Payments developer")).toBeVisible();

  // And sends an enquiry from the profile.
  await client.getByLabel("Subject").fill(subject);
  await client.getByLabel("The work").fill("M-Pesa and Tigo Pesa checkout for an online shop.");
  await client.getByRole("button", { name: "Send enquiry" }).click();
  await expect(
    client.getByRole("status").filter({ hasText: "Your enquiry was sent." }),
  ).toBeVisible();
  await fitsThePhone(client);

  // The developer takes it up and proposes.
  await page.goto("/directory/enquiries");
  await page
    .getByRole("main")
    .getByRole("listitem")
    .filter({ hasText: subject })
    .getByRole("button", { name: "Take it up" })
    .click();
  await page.waitForURL(/\/directory\/engagements\//);
  const engagementPath = new URL(page.url()).pathname;
  await page.getByRole("tab", { name: "Proposal" }).click();
  await page.getByRole("button", { name: "Write a proposal" }).click();
  const form = page.locator("[data-proposal-form]");
  await form.getByLabel("What the work is").fill("Checkout with two mobile-money providers");
  await form.getByRole("textbox", { name: "When", exact: true }).fill("Four weeks");
  await form.getByRole("textbox", { name: "Deliverable 1", exact: true }).fill("Checkout");
  await form.getByLabel("Price").fill("800,000");
  await form.getByRole("button", { name: "Send the proposal" }).click();
  await expect(page.locator("[data-latest-proposal]")).toContainText("Version 1");

  // The client finds it among their engagements and accepts.
  await client.goto("/directory/engagements");
  await client
    .getByRole("main")
    .getByRole("link", { name: new RegExp(subject) })
    .click();
  await expect(client.locator("[data-latest-proposal] [data-total]")).toHaveText(
    /TZS\s800,000\.00/,
  );
  await client.getByRole("button", { name: "Accept" }).click();
  await expect(client.getByText(/You accepted this version/)).toBeVisible();

  // The developer, whose page follows, accepts too: the agreement is recorded.
  await expect(page.getByText(new RegExp(`${people.client} accepted this version`))).toBeVisible({
    timeout: 15_000,
  });
  await page.getByRole("button", { name: "Accept" }).click();
  await expect(page.locator("[data-engagement-state]")).toHaveText("Agreement");
  await expect(client.locator("[data-engagement-state]")).toHaveText("Agreement", {
    timeout: 15_000,
  });
  expect(new URL(client.url()).pathname).toBe(engagementPath);

  // Both export it, and keep the same thing.
  const copies = [];
  for (const phone of [page, client]) {
    const [download] = await Promise.all([
      phone.waitForEvent("download"),
      phone.getByRole("button", { name: "Export the agreement" }).click(),
    ]);
    const copy = JSON.parse(await readFile(await download.path(), "utf8"));
    delete copy.exportedAt;
    copies.push(copy);
  }
  expect(copies[1]).toEqual(copies[0]);
  expect(copies[0].terms.totalMinor).toBe(80_000_000);
  expect(copies[0].disclosure).toBe(
    (await page.locator("[data-disclosure]").textContent())?.trim(),
  );
  await fitsThePhone(page);
  await fitsThePhone(client);

  /*
   * The developer tracks the agreed work in a project of theirs. The project
   * is made over the API — projects have journeys of their own, and this one
   * is about the link — and it is private, with somebody in it who is no
   * party to the engagement.
   */
  const key = `TRK${Date.now().toString(36).slice(-5)}`.toUpperCase();
  const made = await request.post("/api/projects", {
    headers: asDeveloper,
    data: { key, name: `Checkout ${key}`, visibility: "private" },
  });
  expect(made.status(), await made.text()).toBe(201);
  const asColleague = await account(request, people.closed);
  const added = await request.put(`/api/projects/${key}/members/${people.closed}`, {
    headers: asDeveloper,
    data: { role: "member" },
  });
  expect(added.ok(), await added.text()).toBeTruthy();

  // Reloaded, because the page lists the projects it may link to as it opens.
  await page.reload();
  const tracking = page.locator("[data-engagement-project]");
  await tracking.getByRole("combobox", { name: "Track the work in" }).click();
  await page.getByRole("option", { name: new RegExp(key) }).click();
  await tracking.getByRole("button", { name: "Link", exact: true }).click();
  await expect(tracking).toContainText(`${key} · Checkout ${key}`);

  // The client is in no such project, and is not told its name.
  await client.reload();
  await expect(client.locator("[data-engagement-state]")).toHaveText("Agreement");
  await expect(client.getByText(key)).toHaveCount(0);

  // The work starts, the developer says it is done, and it waits on the client,
  // whose phone follows without being reloaded.
  await page.getByRole("button", { name: "Start the work" }).click();
  await expect(page.locator("[data-engagement-state]")).toHaveText("Work under way");
  await expect(client.locator("[data-engagement-state]")).toHaveText("Work under way", {
    timeout: 15_000,
  });
  await page
    .getByRole("button", { name: `The work is complete: ask ${people.client} to confirm` })
    .click();
  await expect(page.locator("[data-engagement-state]")).toHaveText(
    "Completion awaiting confirmation",
  );
  await expect(client.getByText(`${people.developer} says the work is complete`)).toBeVisible({
    timeout: 15_000,
  });
  await client.getByRole("button", { name: "Confirm the work is complete" }).click();
  await expect(client.locator("[data-engagement-state]")).toHaveText("Completed");
  await expect(page.locator("[data-engagement-state]")).toHaveText("Completed", {
    timeout: 15_000,
  });

  // The project leads its members back to the engagement — the developer, who
  // is a party to it.
  await page.goto(`/projects/${key}`);
  await expect(page.getByText("Your engagements tracked here")).toBeVisible();
  // Exactly: the sidebar also lists the enquiry's conversation, by a similar name.
  await expect(page.getByRole("link", { name: subject, exact: true })).toBeVisible();

  // A colleague in the same project sees the project as any member does, and
  // nothing of the engagement: not on the page, not from the API behind it.
  const colleaguesPhone = await secondPhone(browser);
  const colleague = await colleaguesPhone.newPage();
  await signInAs(colleague, people.closed);
  await colleague.goto(`/projects/${key}`);
  await expect(colleague.getByRole("heading", { name: `Checkout ${key}` })).toBeVisible();
  await expect(colleague.getByText("Your engagements tracked here")).toHaveCount(0);
  await expect(colleague.getByText(subject)).toHaveCount(0);
  const linked = await request.get(`/api/directory/projects/${key}/engagements`, {
    headers: asColleague,
  });
  expect(await linked.json()).toEqual([]);
  const engagementId = engagementPath.split("/").pop();
  const opened = await request.get(`/api/directory/engagements/${engagementId}`, {
    headers: asColleague,
  });
  expect(opened.status()).toBe(404);

  await colleaguesPhone.close();
  await clientsPhone.close();
});
