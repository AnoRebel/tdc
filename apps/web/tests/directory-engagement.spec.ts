import { expect, test } from "@playwright/test";

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
 * An enquiry taken up becomes an engagement with a private conversation; the
 * two parties, each in their own browser, watch it change together; and when
 * one withdraws, the other's page shows it withdrawn without a refresh.
 */

onlyInDirectoryProjects();
test.describe.configure({ mode: "serial" });

test("both parties watch an engagement start and close together", async ({
  page,
  request,
  browser,
}) => {
  await founderWithDirectory(request);
  await noEnquiryPending(request);
  const asDeveloper = await account(request, people.developer);
  const asClient = await account(request, people.client);
  const subject = `Payroll export ${Date.now().toString(36)}`;

  const own = await (await request.get("/api/directory/me", { headers: asDeveloper })).json();
  await request.put("/api/directory/me", {
    headers: asDeveloper,
    data: { ...own.profile, visibility: "public", acceptsEnquiries: true },
  });
  const sent = await request.post(`/api/directory/profiles/${people.developer}/enquiries`, {
    headers: asClient,
    data: { subject, body: "An export from our payroll system to the tax portal." },
  });
  expect(sent.status(), await sent.text()).toBe(201);

  // The developer takes it up from their enquiries.
  await signInAs(page, people.developer);
  await page.goto("/directory/enquiries");
  const enquiry = page.getByRole("main").getByRole("listitem").filter({ hasText: subject });
  await enquiry.getByRole("button", { name: "Take it up" }).click();
  await page.waitForURL(/\/directory\/engagements\//);
  await expect(page.locator("[data-engagement-state]")).toHaveText("Discussion");
  const engagementURL = new URL(page.url()).pathname;
  await fitsThePhone(page);

  // The client, on their own phone, finds it in their engagements and talks.
  const clientsPhone = await secondPhone(browser);
  const client = await clientsPhone.newPage();
  await signInAs(client, people.client);
  await client.goto("/directory/engagements");
  await client
    .getByRole("main")
    .getByRole("link", { name: new RegExp(subject) })
    .click();
  await expect(client.locator("[data-engagement-state]")).toHaveText("Discussion");
  await expect(client.getByText("An export from our payroll system")).toBeVisible();
  await client.getByRole("textbox", { name: /message/i }).fill("Thank you for taking this up");
  await client.keyboard.press("Enter");
  await expect(page.getByText("Thank you for taking this up")).toBeVisible();

  // The developer withdraws; the client's page follows without a reload.
  await page.getByRole("button", { name: "Withdraw" }).click();
  await page.getByPlaceholder(/Why, if you want to say/).fill("Too much on this month");
  await page.getByRole("dialog").getByRole("button", { name: "Withdraw" }).click();
  await expect(page.locator("[data-engagement-state]")).toHaveText("Withdrawn");
  await expect(client.locator("[data-engagement-state]")).toHaveText("Withdrawn", {
    timeout: 15_000,
  });
  expect(new URL(client.url()).pathname).toBe(engagementURL);

  // Withdrawn for both, with who and why, and no agreement.
  await client.getByRole("button", { name: "History" }).click();
  await expect(client.locator("[data-engagement-history]")).toContainText("Too much on this month");
  const record = await (
    await request.get(`/api/directory${engagementURL.replace("/directory", "")}`, {
      headers: asClient,
    })
  ).json();
  expect(record.state).toBe("withdrawn");
  expect(record.agreement).toBeUndefined();
  await fitsThePhone(client);
});
