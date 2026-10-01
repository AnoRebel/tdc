import { devices, expect, test } from "@playwright/test";

import {
  account,
  fitsThePhone,
  founderWithDirectory,
  onlyInDirectoryProjects,
  people,
  signInAs,
} from "./directory.helpers";

/*
 * First contact on a phone: a client sends an enquiry from a developer's
 * profile, the developer reads it, and a developer who takes no enquiries is
 * shown as such rather than offered a form that would be refused.
 */

onlyInDirectoryProjects();
test.describe.configure({ mode: "serial" });

const subject = `Clinic booking ${Date.now().toString(36)}`;

test("a client's enquiry reaches the developer, who can read it", async ({
  page,
  request,
  browser,
}) => {
  await founderWithDirectory(request);
  const asDeveloper = await account(request, people.developer);
  await account(request, people.client);

  const own = await (await request.get("/api/directory/me", { headers: asDeveloper })).json();
  const opened = await request.put("/api/directory/me", {
    headers: asDeveloper,
    data: { ...own.profile, visibility: "public", acceptsEnquiries: true },
  });
  expect(opened.ok()).toBeTruthy();

  await signInAs(page, people.client);
  await page.goto(`/directory/people/${people.developer}`);
  await page.getByLabel("Subject").fill(subject);
  await page.getByLabel("The work").fill("A booking app for three clinics in Mwanza, by March.");
  await page.getByRole("button", { name: "Send enquiry" }).click();
  await expect(page.getByRole("status")).toContainText("Your enquiry was sent.");
  await fitsThePhone(page);

  await page.goto("/directory/enquiries?box=sent");
  await expect(page.getByText(subject)).toBeVisible();
  await expect(page.getByText("Waiting for an answer").first()).toBeVisible();

  // The developer, on their own phone.
  const developersPhone = await browser.newContext({ ...devices["Pixel 7"] });
  const developer = await developersPhone.newPage();
  await signInAs(developer, people.developer);
  await developer.goto("/directory/enquiries");
  // Scoped to the page: the conversation it opened is in the sidebar too.
  const received = developer.getByRole("main").getByRole("listitem").filter({ hasText: subject });
  await expect(received).toBeVisible();
  await received.getByRole("link", { name: "Open the conversation" }).click();
  await expect(developer.getByText("A booking app for three clinics in Mwanza")).toBeVisible();

  // Notified the way any direct message notifies.
  const notifications = await (
    await request.get("/api/me/notifications", { headers: asDeveloper })
  ).json();
  expect(notifications.some((entry: { reason: string }) => entry.reason === "direct")).toBe(true);
});

test("a developer taking no enquiries is not offered one", async ({ page, request }) => {
  await founderWithDirectory(request);
  const asClosed = await account(request, people.closed);
  const asClient = await account(request, people.client);

  const own = await (await request.get("/api/directory/me", { headers: asClosed })).json();
  const closed = await request.put("/api/directory/me", {
    headers: asClosed,
    data: { ...own.profile, visibility: "members", acceptsEnquiries: false },
  });
  expect(closed.ok()).toBeTruthy();

  await signInAs(page, people.client);
  await page.goto(`/directory/people/${people.closed}`);
  await expect(page.getByText(/is not taking enquiries through the directory/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Send enquiry" })).toHaveCount(0);
  await fitsThePhone(page);

  // And the server says the same to anybody who asks without the form.
  const refused = await request.post(`/api/directory/profiles/${people.closed}/enquiries`, {
    headers: asClient,
    data: { subject: "Work", body: "Are you free?" },
  });
  expect(refused.status()).toBe(403);
  expect((await refused.json()).message).toContain("not accepting enquiries");
});
