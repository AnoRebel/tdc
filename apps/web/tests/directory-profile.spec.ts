import { expect, test } from "@playwright/test";

import {
  account,
  fitsThePhone,
  founderWithDirectory,
  onlyInDirectoryProjects,
  people,
  signInAs,
} from "./directory.helpers";

/*
 * A developer writes a profile on a phone, and restricts who sees it.
 *
 * The server's own tests prove each visibility rule at the HTTP boundary. What
 * they cannot show is that the form in front of them works: that the choices
 * reach the server, that the saved profile comes back into the form, and that
 * the whole thing is usable at the width most people will use it at.
 */

onlyInDirectoryProjects();

const developer = people.developer;

test.describe.configure({ mode: "serial" });

test("a profile is created, edited and restricted on a phone", async ({ page, request }) => {
  const founder = await founderWithDirectory(request);
  const asDeveloper = await account(request, developer);
  // Other specs give this person a profile too; this one starts without.
  await request.delete("/api/directory/me", { headers: asDeveloper });

  await signInAs(page, developer);
  await page.goto("/directory/profile");

  // No profile is a complete state, and the page says what one is for.
  await expect(page.getByRole("heading", { name: "You have no directory profile" })).toBeVisible();
  await fitsThePhone(page);

  await page.getByLabel("Headline").fill("Mobile developer");
  await page.getByLabel("Location").fill("Mwanza");
  await page.getByLabel("About", { exact: true }).fill("Flutter and Kotlin, mostly for NGOs.");
  await page.getByRole("button", { name: "Add a link" }).click();
  await page.getByLabel("Title").fill("Clinic booking app");
  await page.getByLabel("Link", { exact: true }).fill("https://example.com/clinic");
  await page.getByLabel("Email").fill(`${developer}@example.com`);

  // Found by anyone; contact details left at their default.
  await page
    .getByRole("group", { name: "Who can find you" })
    .getByText("Anyone", { exact: true })
    .click();
  await page
    .getByRole("group", { name: "Who sees what you wrote" })
    .getByText("Anyone", { exact: true })
    .click();

  await page.getByRole("button", { name: "Create profile" }).click();
  await expect(page.getByRole("status")).toHaveText("Your profile is created.");
  await fitsThePhone(page);

  // What a member sees: the profile, the portfolio, and no contact details.
  const asMember = await request.get(`/api/directory/profiles/${developer}`, { headers: founder });
  expect(asMember.ok()).toBeTruthy();
  const shown = await asMember.json();
  expect(shown.headline).toBe("Mobile developer");
  expect(shown.portfolio).toHaveLength(1);
  expect(shown.contactEmail, "contact details defaulted to hidden").toBeUndefined();

  // Restricting what was written: saved, and absent from what a member sees.
  await page
    .getByRole("group", { name: "Who sees what you wrote" })
    .getByText("Only you", { exact: true })
    .click();
  await page.getByRole("button", { name: "Save profile" }).click();
  await expect(page.getByRole("status")).toHaveText("Your profile is saved.");

  // The form came back holding what the server kept, not what was typed.
  await expect(page.getByLabel("Headline")).toHaveValue("Mobile developer");
  await expect(
    page.getByRole("group", { name: "Who sees what you wrote" }).getByRole("radio", {
      name: /Only you/,
    }),
  ).toBeChecked();

  const restricted = await (
    await request.get(`/api/directory/profiles/${developer}`, { headers: founder })
  ).json();
  expect(restricted.headline).toBeUndefined();
  expect(restricted.portfolio).toBeUndefined();

  // And the page showing it as a member would, drawn from the same response.
  await page.getByRole("link", { name: "See it as a member would" }).click();
  await expect(page.getByRole("heading", { level: 1, name: developer })).toBeVisible();
  await expect(page.getByText("Mobile developer")).toHaveCount(0);
  await expect(page.getByText("Clinic booking app")).toHaveCount(0);
  await fitsThePhone(page);
});

test("a company is created, and lists only people who accepted", async ({ page, request }) => {
  await founderWithDirectory(request);
  await account(request, developer);
  const owner = await account(request, people.client);
  const slug = `kilima-${Date.now().toString(36)}`;

  // The owner makes the company and invites the developer, over the API.
  const created = await request.post("/api/directory/companies", {
    headers: owner,
    data: { slug, name: "Kilima Labs", location: "Arusha", visibility: "members", role: "Founder" },
  });
  expect(created.status()).toBe(201);
  const invited = await request.post(`/api/directory/companies/${slug}/members`, {
    headers: owner,
    data: { username: developer, role: "Mobile lead" },
  });
  expect(invited.status()).toBe(202);

  await signInAs(page, developer);

  // Not listed yet: the company asked, and nobody has answered.
  await page.goto(`/directory/companies/${slug}`);
  await expect(page.getByRole("heading", { name: "Kilima Labs" }).first()).toBeVisible();
  await expect(page.getByRole("link", { name: developer })).toHaveCount(0);

  // The developer answers from their own page.
  await page.goto("/directory/profile");
  const invitation = page.getByRole("listitem").filter({ hasText: "Kilima Labs" });
  await expect(invitation.getByText(/Asked you to be listed/)).toBeVisible();
  await invitation.getByRole("button", { name: "Accept" }).click();
  await expect(invitation.getByRole("button", { name: "Leave" })).toBeVisible();
  await fitsThePhone(page);

  await page.goto(`/directory/companies/${slug}`);
  await expect(page.getByRole("link", { name: developer })).toBeVisible();
  await fitsThePhone(page);
});
