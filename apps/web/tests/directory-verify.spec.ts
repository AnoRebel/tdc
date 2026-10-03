import { expect, type Page, test } from "@playwright/test";

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
 * A moderator verifies one claim among several, and withdraws another, entirely
 * through the interface — and the statement of what a verification means reads
 * the same on every screen that shows one.
 */

onlyInDirectoryProjects();
test.describe.configure({ mode: "serial" });

const developer = people.developer;

/** Opens the first verified badge on the page and returns what it says. */
async function meaningOn(page: Page) {
  await page.locator("[data-verified] button").first().click();
  const meaning = page.locator("[data-verification-meaning]").first();
  await expect(meaning).toBeVisible();
  return (await meaning.innerText()).trim();
}

test("a moderator verifies one claim and revokes another from the interface", async ({
  page,
  request,
  browser,
}) => {
  await founderWithDirectory(request);
  const asDeveloper = await account(request, developer);
  await account(request, people.client);

  // Three skills and a link, all self-asserted to begin with.
  const saved = await request.put("/api/directory/me", {
    headers: asDeveloper,
    data: {
      visibility: "members",
      detailsVisibility: "members",
      skills: [
        { name: "Go", description: "eight years" },
        { name: "PostgreSQL", description: "six years" },
        { name: "Docker", description: "every day" },
      ],
      portfolio: [{ title: "Clinic booking", url: "https://example.com/clinic" }],
    },
  });
  expect(saved.ok()).toBeTruthy();

  // The founder holds every permission, and verifies from the review page.
  await signInAs(page, "founder");
  await page.goto("/directory/review");
  await page.getByLabel("Username").fill(developer);
  await page.getByRole("button", { name: "Open profile" }).click();

  const goClaim = page.getByRole("listitem").filter({ hasText: /^Skill\s*Go/ });
  await goClaim.getByLabel("What you checked for Go").fill("Reviewed their public repositories");
  await goClaim.getByRole("button", { name: "Verify" }).click();
  await expect(goClaim.getByRole("button", { name: "Verified" })).toBeVisible();

  const link = page.getByRole("listitem").filter({ hasText: "Clinic booking" });
  await link.getByLabel("What you checked for Clinic booking").fill("Visited the site");
  await link.getByRole("button", { name: "Verify" }).click();
  await expect(link.getByRole("button", { name: "Verified" })).toBeVisible();
  const onReview = await meaningOn(page);
  await fitsThePhone(page);

  // Withdrawn, with a reason, and gone.
  await link
    .getByLabel("Why you are revoking Clinic booking")
    .fill("The site belongs to someone else");
  await link.getByRole("button", { name: "Revoke" }).click();
  await expect(link.getByRole("button", { name: "Verified" })).toHaveCount(0);

  // A client sees one verified skill among three, and the same statement.
  const clientsPhone = await secondPhone(browser);
  const client = await clientsPhone.newPage();
  await signInAs(client, people.client);
  await client.goto(`/directory/people/${developer}`);
  const skills = client.locator("section").filter({ hasText: "Skills" });
  await expect(skills.locator("[data-verified]")).toHaveCount(1);
  await expect(
    skills.getByRole("listitem").filter({ hasText: "Go" }).locator("[data-verified]"),
  ).toHaveCount(1);
  await expect(client.getByText("Visited the site")).toHaveCount(0);
  const onProfile = await meaningOn(client);
  await expect(client.locator("[data-verification-note]")).toHaveCount(0);
  await fitsThePhone(client);

  // The developer sees it on their own page, with what was checked.
  const developersPhone = await secondPhone(browser);
  const own = await developersPhone.newPage();
  await signInAs(own, developer);
  await own.goto("/directory/profile");
  const onOwnPage = await meaningOn(own);
  await expect(own.locator("[data-verification-note]").first()).toContainText(
    "Reviewed their public repositories",
  );

  // One verification, read on three screens: the same words on each.
  for (const [screen, text] of [
    ["the review page", onReview],
    ["the profile a client reads", onProfile],
    ["the owner's own page", onOwnPage],
  ]) {
    expect(text, `the statement on ${screen}`).toMatch(
      /checked the evidence for this one claim .* It is not an endorsement of the person/,
    );
  }
  expect(
    new Set([onReview, onProfile, onOwnPage]).size,
    "the statement differs between screens",
  ).toBe(1);
});
