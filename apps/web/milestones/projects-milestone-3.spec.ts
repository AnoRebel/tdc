import { expect, test } from "@playwright/test";

import { choose, openTab, signIn } from "./helpers";

/*
 * An issue carries enough structure to be tracked — all set in the browser.
 *
 * Two people assigned, two labels and a priority; a child whose completion the
 * parent reflects; a blocking relationship, and a second one that would close a
 * loop, refused with the loop named. The script that runs this made the
 * project, its three issues and its two members, and checks afterwards that the
 * two people were told.
 */
test("an issue is assigned, labelled, prioritised and related, and a loop is refused", async ({
  page,
}) => {
  await signIn(page, "founder");

  await test.step("the project's labels: bug and frontend", async () => {
    await page.goto("/projects/WEB");
    await openTab(page, "Settings");
    for (const name of ["bug", "frontend"]) {
      await page.getByLabel("New label").fill(name);
      await page.getByRole("button", { name: "Add label" }).click();
      await expect(page.getByRole("button", { name: `Remove the label ${name}` })).toBeVisible();
    }
  });

  const fields = page.getByRole("complementary", { name: "Details" });

  await test.step("assign two people", async () => {
    await page.goto("/issues/WEB-1");
    await choose(page, "Assign somebody", "amina");
    await expect(fields.getByRole("button", { name: "Take amina off this issue" })).toBeVisible();
    await choose(page, "Assign somebody", "bola");
    await expect(fields.getByRole("button", { name: "Take bola off this issue" })).toBeVisible();
  });

  await test.step("two labels and a priority", async () => {
    for (const name of ["bug", "frontend"]) {
      const toggle = fields.getByRole("button", { name, exact: true });
      await toggle.click();
      await expect(toggle).toHaveAttribute("aria-pressed", "true");
    }
    await choose(page, "Priority", "Urgent");
    await expect(page.getByRole("combobox", { name: "Priority" })).toContainText("Urgent");
  });

  await test.step("a child, and a blocking relationship", async () => {
    await choose(page, "How they relate", "This is the parent of");
    await fields.getByLabel("The other issue").fill("WEB-2");
    await fields.getByRole("button", { name: "Relate", exact: true }).click();
    await expect(fields.getByText("0 of 1 parts done")).toBeVisible();

    await choose(page, "How they relate", "This blocks");
    await fields.getByLabel("The other issue").fill("WEB-3");
    await fields.getByRole("button", { name: "Relate", exact: true }).click();
    await expect(fields.getByRole("link", { name: /WEB-3/ })).toBeVisible();
  });

  await test.step("a loop is refused, and says where the loop would be", async () => {
    await page.goto("/issues/WEB-3");
    await choose(page, "How they relate", "This blocks");
    await fields.getByLabel("The other issue").fill("WEB-1");
    await fields.getByRole("button", { name: "Relate", exact: true }).click();
    const refusal = fields.getByRole("alert");
    await expect(refusal).toContainText("loop");
    await expect(refusal).toContainText("WEB-1");
    await expect(refusal).toContainText("WEB-3");
    // And nothing was recorded: WEB-3 still blocks nothing.
    await expect(fields.getByText("Blocks", { exact: true })).toHaveCount(0);
  });

  await test.step("the child's completion shows on the parent", async () => {
    await page.goto("/issues/WEB-2");
    await choose(page, "State", "Done");
    await expect(page.getByRole("combobox", { name: "State" })).toContainText("Done");

    await page.goto("/issues/WEB-1");
    await expect(fields.getByText("1 of 1 parts done")).toBeVisible();
    const history = page.getByRole("region", { name: "History" });
    await expect(history).toContainText("founder assigned amina");
    await expect(history).toContainText("founder assigned bola");
    await expect(history).toContainText("founder set the priority to Urgent");
  });

  await page.screenshot({ path: "test-results/projects-milestone-3.png", fullPage: true });
});
