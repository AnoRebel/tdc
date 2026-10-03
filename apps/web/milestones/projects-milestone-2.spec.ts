import { expect, test, type Page } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * Issues talk back: the conversation link, both directions.
 *
 * Two people discuss a bug in a private channel; one turns the message into an
 * issue. A member of the channel opening the issue sees the message it came
 * from; somebody outside the channel sees only that it came from somewhere they
 * cannot look. And a comment on the issue, with a mention in it, is posted
 * through the same conversation every channel uses.
 *
 * The script that runs this made the channel, the project and the accounts; this
 * is everything a person does with them.
 */

const BUG = "The search page times out when the query has an apostrophe";

async function openChannel(page: Page, name: string) {
  await page.goto("/");
  await page.getByRole("navigation").getByRole("link", { name, exact: true }).first().click();
  const composer = page.getByRole("textbox", { name: /message/i });
  await composer.waitFor();
  return composer;
}

test("a message becomes an issue, and the issue remembers it without disclosing it", async ({
  browser,
}) => {
  const aminaContext = await browser.newContext();
  const bolaContext = await browser.newContext();
  const outsiderContext = await browser.newContext();
  const amina = await aminaContext.newPage();
  const bola = await bolaContext.newPage();
  const outsider = await outsiderContext.newPage();

  try {
    await test.step("two people discuss a bug in a channel", async () => {
      await signIn(amina, "amina");
      await signIn(bola, "bola");

      const aminaSays = await openChannel(amina, "backend");
      const bolaSays = await openChannel(bola, "backend");

      await aminaSays.fill(`${BUG}\nIt happens for every name like O'Brien.`);
      await amina.keyboard.press("Enter");
      await expect(bola.getByText(BUG)).toBeVisible({ timeout: 20_000 });

      await bolaSays.fill("Reproduced it here too.");
      await bola.keyboard.press("Enter");
      await expect(amina.getByText("Reproduced it here too.")).toBeVisible({ timeout: 20_000 });
    });

    await test.step("one of them turns the message into an issue", async () => {
      await bola.getByText(BUG).click({ button: "right" });
      await bola.getByRole("menuitem", { name: "Create an issue from this" }).click();

      const dialog = bola.getByRole("dialog", { name: "Create an issue from this message" });
      await expect(dialog.getByLabel("Title")).toHaveValue(BUG);
      await expect(dialog.getByLabel("Description")).toHaveValue(
        "It happens for every name like O'Brien.",
      );
      await dialog.getByRole("button", { name: "New issue" }).click();

      await bola.waitForURL("**/issues/WEB-1");
      await expect(bola.getByRole("heading", { name: BUG })).toBeVisible();
    });

    await test.step("a channel member sees the message it came from", async () => {
      const origin = bola.getByRole("region", { name: "Where this came from" });
      await expect(origin).toContainText(BUG);
      await expect(origin).toContainText("amina");
      await expect(origin.getByRole("link", { name: "Open it in backend" })).toBeVisible();
    });

    await test.step("and the message shows the issue it became", async () => {
      await amina.reload();
      await expect(amina.getByRole("link", { name: `Tracked as WEB-1: ${BUG}` })).toBeVisible({
        timeout: 20_000,
      });
    });

    await test.step("somebody outside the channel sees only that there was a conversation", async () => {
      await signIn(outsider, "outsider");
      await outsider.goto("/issues/WEB-1");
      await expect(outsider.getByRole("heading", { name: BUG })).toBeVisible();

      const origin = outsider.getByRole("region", { name: "Where this came from" });
      await expect(origin).toContainText(
        "This issue was made from a message in a conversation you cannot see.",
      );
      // Not the channel's name, not who said it, not what was said. (The
      // description is bola's own words, copied deliberately when filing; the
      // reference itself carries nothing.)
      await expect(origin).not.toContainText("backend");
      await expect(origin).not.toContainText("amina");
      await expect(origin).not.toContainText(BUG);
      await expect(origin.getByRole("link")).toHaveCount(0);
    });

    await test.step("a comment on the issue is a channel message, mention included", async () => {
      // Bola moves on to something else; somebody looking at the discussion
      // is not interrupted about it, exactly as in a channel.
      await bola.goto("/projects");
      await amina.goto("/issues/WEB-1");
      const discussion = amina.getByRole("region", { name: "Discussion" });
      await discussion.getByRole("button", { name: "Join" }).click();
      const composer = discussion.getByRole("textbox", { name: /message/i });
      await composer.fill("@bola can you take this one? The query builder quotes it twice.");
      await amina.keyboard.press("Enter");
      await expect(discussion.getByText("The query builder quotes it twice.")).toBeVisible();

      // Edited exactly as a channel message is edited.
      await discussion.getByText("The query builder quotes it twice.").click({ button: "right" });
      await amina.getByRole("menuitem", { name: "Edit", exact: true }).click();
      const editor = discussion.getByRole("textbox", { name: "Edit this message" });
      await editor.fill("@bola can you take this one? The query builder escapes it twice.");
      await editor.press("Enter");
      await expect(discussion.getByText("escapes it twice.")).toBeVisible();
      await expect(discussion.getByText("edited")).toBeVisible();
    });
  } finally {
    await aminaContext.close();
    await bolaContext.close();
    await outsiderContext.close();
  }
});
