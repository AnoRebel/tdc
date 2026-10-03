import { readFile } from "node:fs/promises";

import { expect, test } from "@playwright/test";
import type { APIRequestContext, Page } from "@playwright/test";

import {
  fitsThePhone,
  founderWithDirectory,
  existing,
  noEnquiryPending,
  onlyInDirectoryProjects,
  password,
  people,
  secondPhone,
  signInAs,
} from "./directory.helpers";

/*
 * The whole directory, as the user guide describes it, by two people on two
 * phones at once and through nothing but the interface: each joins the server,
 * the developer makes a profile anyone can find, the client finds it by skill
 * and place and sends an enquiry, the developer takes it up and proposes, both
 * accept, and both export the same agreement.
 *
 * The other directory specs set their scenes over the API so that each checks
 * one thing; this one sets up nothing, because what it checks is that the
 * steps a person would take connect.
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

test("two people go from a profile to an exported agreement through the interface", async ({
  page,
  request,
  browser,
}) => {
  await founderWithDirectory(request);
  const subject = `Mobile money checkout ${Date.now().toString(36)}`;
  const place = `Dodoma${Date.now().toString(36)}`;

  // The developer joins and writes a profile anyone can find.
  await join(page, request, people.developer);
  const asDeveloper = await existing(request, people.developer);
  if (!asDeveloper) throw new Error("the developer could not sign in after joining");
  // Earlier specs may have given this person a profile; the guide starts without one.
  await request.delete("/api/directory/me", { headers: asDeveloper });
  await page.goto("/directory/profile");
  await page.getByLabel("Headline").fill("Payments developer");
  await page.getByLabel("Location").fill(place);
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
  await clientsPhone.close();
});
