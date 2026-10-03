import { expect, test } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * After Sprint 1 has ended, as the browser shows it.
 *
 * The script finished WEB-1 and WEB-2, moved Sprint 1's last day to
 * yesterday, and waited for the server to deal with the end by itself. Here:
 * the ended cycle says what happened, the unfinished issues are in Sprint 2,
 * each one's history says the server carried it, and the milestone — its day
 * passed — is overdue, spanning both cycles, with nothing about its issues
 * changed.
 */

test("an ended cycle carried its unfinished work forward, and an overdue milestone changed nothing", async ({
  page,
}) => {
  await signIn(page, "founder");

  await test.step("Sprint 1 has ended, and says how", async () => {
    await page.goto("/projects/WEB/cycles");
    const sprint1 = page.getByRole("listitem").filter({ hasText: "Sprint 1" });
    await expect(sprint1.locator("[data-status]")).toHaveText("Ended");
    await expect(sprint1).toContainText("2 of 2 done");
    await sprint1.getByRole("link", { name: "Sprint 1" }).click();
    await expect(page.locator("[data-closed]")).toContainText(
      "unfinished issues carry forward to the next cycle",
    );
    const inside = page
      .getByRole("region", { name: "Issues in this cycle" })
      .locator("li .font-mono");
    await expect(inside).toHaveCount(2);
    expect((await inside.allInnerTexts()).sort()).toEqual(["WEB-1", "WEB-2"]);
  });

  await test.step("every unfinished issue is in Sprint 2", async () => {
    await page.goto("/projects/WEB/cycles");
    await page.getByRole("link", { name: "Sprint 2", exact: true }).click();
    const inside = page
      .getByRole("region", { name: "Issues in this cycle" })
      .locator("li .font-mono");
    await expect(inside).toHaveCount(4);
    expect((await inside.allInnerTexts()).sort()).toEqual(["WEB-3", "WEB-4", "WEB-5", "WEB-6"]);
  });

  await test.step("and each says the server carried it, and when", async () => {
    for (const key of ["WEB-3", "WEB-6"]) {
      await page.goto(`/issues/${key}`);
      await expect(
        page.getByText("When Sprint 1 ended, the server carried this forward to Sprint 2"),
      ).toBeVisible();
    }
  });

  await test.step("Beta is overdue, spans both cycles, and its issues are as they were", async () => {
    await page.goto("/projects/WEB/milestones");
    const beta = page.getByRole("listitem").filter({ hasText: "Beta" });
    await expect(beta.locator("[data-overdue]")).toHaveText("Overdue");
    await expect(beta).toContainText("1 of 2 done");
    await beta.getByRole("link", { name: "Beta" }).click();
    const spans = page.getByRole("region", { name: "Cycles it spans" }).getByRole("link");
    await expect(spans).toHaveCount(2);
    await expect(spans.nth(0)).toContainText("Sprint 1");
    await expect(spans.nth(1)).toContainText("Sprint 2");
    await expect(page.getByText(/Nothing about the issues has been changed/)).toBeVisible();
    const issues = page
      .getByRole("region", { name: "Issues in this milestone" })
      .getByRole("listitem");
    await expect(issues.filter({ hasText: "WEB-1" })).toContainText("Done");
    await expect(issues.filter({ hasText: "WEB-6" })).toContainText("To do");
  });
});
