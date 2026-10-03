import { readFile } from "node:fs/promises";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

import {
  account,
  fitsThePhone,
  founderWithDirectory,
  noEnquiryPending,
  onlyInDirectoryProjects,
  people,
  secondPhone,
  signInAs,
} from "./directory.helpers";

/*
 * From a proposal to finished work, by two people on two phones.
 *
 * The developer proposes; the client asks for one line to change; the developer
 * sends a new version with that line changed; both accept it and the agreement
 * is recorded; both export it and get the same terms under the same statement
 * the screen showed; the developer says the work is complete, and it stays
 * outstanding until the client confirms. Each person's page follows the other's
 * changes without being reloaded.
 */

onlyInDirectoryProjects();
test.describe.configure({ mode: "serial" });

async function exported(page: Page) {
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    page.getByRole("button", { name: "Export the agreement" }).click(),
  ]);
  const path = await download.path();
  return JSON.parse(await readFile(path, "utf8")) as Record<string, unknown>;
}

test("a proposal is revised, agreed, exported and completed by both", async ({
  page,
  request,
  browser,
}) => {
  await founderWithDirectory(request);
  await noEnquiryPending(request);
  const asDeveloper = await account(request, people.developer);
  const asClient = await account(request, people.client);
  const subject = `Clinic bookings ${Date.now().toString(36)}`;

  // An engagement to work in, set up over the API: the enquiry and taking it
  // up have their own spec.
  const own = await (await request.get("/api/directory/me", { headers: asDeveloper })).json();
  await request.put("/api/directory/me", {
    headers: asDeveloper,
    data: { ...own.profile, visibility: "public", acceptsEnquiries: true },
  });
  const enquiry = await request.post(`/api/directory/profiles/${people.developer}/enquiries`, {
    headers: asClient,
    data: { subject, body: "Online booking for three clinics." },
  });
  expect(enquiry.status(), await enquiry.text()).toBe(201);
  const taken = await request.post(`/api/directory/enquiries/${(await enquiry.json()).id}/accept`, {
    headers: asDeveloper,
  });
  expect(taken.ok(), await taken.text()).toBeTruthy();
  const engagement = (await taken.json()).id as string;

  // The developer writes the first version on their phone.
  await signInAs(page, people.developer);
  await page.goto(`/directory/engagements/${engagement}`);
  await page.getByRole("tab", { name: "Proposal" }).click();
  await page.getByRole("button", { name: "Write a proposal" }).click();
  const form = page.locator("[data-proposal-form]");
  await form.getByLabel("What the work is").fill("Online booking for three clinics");
  await form.getByRole("textbox", { name: "When", exact: true }).fill("Six weeks from agreement");
  await form.getByRole("textbox", { name: "Deliverable 1", exact: true }).fill("Booking screens");
  await form.getByLabel("Price").first().fill("1,500,000");
  await form.getByRole("button", { name: "Add a deliverable" }).click();
  await form.getByRole("textbox", { name: "Deliverable 2", exact: true }).fill("Reports");
  await form.getByLabel("Price").nth(1).fill("500000");
  await expect(form.locator("[data-proposal-total]")).toContainText("TZS 2,000,000.00");
  await fitsThePhone(page);
  await form.getByRole("button", { name: "Send the proposal" }).click();
  await expect(page.locator("[data-latest-proposal]")).toContainText("Version 1");
  await expect(page.locator("[data-engagement-state]")).toHaveText("Proposal");

  // The client opens it on their own phone and asks for the second line to change.
  const clientsPhone = await secondPhone(browser);
  const client = await clientsPhone.newPage();
  await signInAs(client, people.client);
  await client.goto(`/directory/engagements/${engagement}`);
  await expect(client.getByRole("tab", { name: "Proposal" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(client.locator("[data-latest-proposal] [data-total]")).toHaveText(
    /TZS\s2,000,000\.00/,
  );
  await client.getByRole("button", { name: "Ask for a change" }).click();
  await client.getByRole("combobox", { name: "What should change" }).click();
  await client.getByRole("option", { name: /Deliverable 2: Reports/ }).click();
  await client.getByLabel("What you would like instead").fill("Monthly totals per clinic, please");
  await client.getByRole("button", { name: "Send the request" }).click();
  await expect(client.locator("[data-engagement-state]")).toHaveText("Negotiation");

  // The developer sees the request against that line without reloading, and
  // sends a second version changing only it.
  await expect(page.locator("[data-latest-proposal]")).toContainText(
    `${people.client} asked for a change to Deliverable 2`,
    { timeout: 15_000 },
  );
  await expect(page.locator("[data-latest-proposal]")).toContainText("Monthly totals per clinic");
  await page.getByRole("button", { name: "Send a new version" }).click();
  await expect(form.getByRole("textbox", { name: "Deliverable 1", exact: true })).toHaveValue(
    "Booking screens",
  );
  await form
    .getByRole("textbox", { name: "Deliverable 2", exact: true })
    .fill("Reports with monthly totals per clinic");
  await form.getByLabel("Price").nth(1).fill("650,000");
  await form.getByRole("button", { name: "Send the proposal" }).click();
  const latest = page.locator("[data-latest-proposal]");
  await expect(latest).toContainText("Version 2");
  await expect(latest.locator("[data-line]").nth(1).locator("[data-changed]")).toBeVisible();
  await expect(latest.locator("[data-line]").nth(0).locator("[data-changed]")).toHaveCount(0);

  // The client sees version two and accepts. One acceptance agrees nothing.
  const clientsLatest = client.locator("[data-latest-proposal]");
  await expect(clientsLatest).toContainText("Version 2", { timeout: 15_000 });
  await expect(clientsLatest.locator("[data-total]")).toHaveText(/TZS\s2,150,000\.00/);
  await client.getByRole("button", { name: "Accept" }).click();
  await expect(client.getByText(/You accepted this version/)).toBeVisible();
  await expect(client.locator("[data-agreement]")).toHaveCount(0);

  // The developer accepts the same version; both now see the agreement.
  await expect(page.getByText(new RegExp(`${people.client} accepted this version`))).toBeVisible({
    timeout: 15_000,
  });
  await page.getByRole("button", { name: "Accept" }).click();
  await expect(page.locator("[data-agreement]")).toContainText("Both of you accepted version 2");
  await expect(client.locator("[data-agreement]")).toBeVisible({ timeout: 15_000 });
  await expect(page.locator("[data-engagement-state]")).toHaveText("Agreement");

  // The same statement on both screens, and in both exports.
  const shown = await page.locator("[data-disclosure]").textContent();
  expect(shown).toContain("not a party to this agreement");
  await expect(client.locator("[data-disclosure]")).toHaveText(shown ?? "");
  const developersCopy = await exported(page);
  const clientsCopy = await exported(client);
  expect(developersCopy.format).toBe("tdc.directory.agreement/1");
  expect(developersCopy.disclosure).toBe(shown?.trim());
  const { exportedAt: _a, ...developersTerms } = developersCopy;
  const { exportedAt: _b, ...clientsTerms } = clientsCopy;
  expect(clientsTerms).toEqual(developersTerms);
  expect((developersCopy.terms as { totalMinor: number }).totalMinor).toBe(215_000_000);
  await fitsThePhone(page);
  await fitsThePhone(client);

  // The work starts, the developer says it is done, and it waits on the client.
  await page.getByRole("button", { name: "Start the work" }).click();
  await expect(page.locator("[data-engagement-state]")).toHaveText("Work under way");
  await page.getByLabel(`A note for ${people.client}, if you want`).fill("Deployed to all three");
  await page
    .getByRole("button", { name: `The work is complete: ask ${people.client} to confirm` })
    .click();
  await expect(page.locator("[data-completion-outstanding]")).toBeVisible();
  await expect(page.locator("[data-engagement-state]")).toHaveText(
    "Completion awaiting confirmation",
  );
  await expect(page.getByRole("button", { name: "Confirm the work is complete" })).toHaveCount(0);

  await expect(client.getByText(`${people.developer} says the work is complete`)).toBeVisible({
    timeout: 15_000,
  });
  await client.getByRole("button", { name: "Confirm the work is complete" }).click();
  await expect(client.locator("[data-engagement-state]")).toHaveText("Completed");
  await expect(page.locator("[data-engagement-state]")).toHaveText("Completed", {
    timeout: 15_000,
  });

  // The history has every step, with who took it.
  await client.getByRole("button", { name: "History" }).click();
  const history = client.locator("[data-engagement-history]");
  for (const step of ["Proposal", "Negotiation", "Agreement", "Work under way", "Completed"]) {
    await expect(history).toContainText(step);
  }
  await expect(history).toContainText("Deployed to all three");
  await clientsPhone.close();
});
