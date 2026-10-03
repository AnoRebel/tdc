import { expect, test, type Page } from "@playwright/test";

/*
 * Whether somebody who never touches a mouse can work this.
 *
 * Not a rule axe can check: it reads markup, and this is about what actually
 * happens when Tab is pressed. A control that cannot be reached is a control
 * that does not exist for the person using it, however correct its label.
 */

/**
 * Everything Tab can reach from here, in the order it is reached.
 *
 * Bounded, because a trap would otherwise hang the run rather than fail it —
 * and a hang is the least useful way to report a problem.
 */
async function reachableByTab(page: Page, limit = 60): Promise<string[]> {
  const reached: string[] = [];

  for (let i = 0; i < limit; i++) {
    await page.keyboard.press("Tab");

    const focused = await page.evaluate(() => {
      const el = document.activeElement;
      if (!el || el === document.body) return null;

      // What somebody would call this thing, in the order a screen reader
      // would find a name for it.
      const name =
        el.getAttribute("aria-label") ??
        el.textContent?.trim().slice(0, 40) ??
        el.getAttribute("placeholder") ??
        "";

      return `${el.tagName.toLowerCase()}:${name}`;
    });

    if (focused === null) break;
    if (reached.includes(focused) && reached[0] === focused) break;
    reached.push(focused);
  }

  return reached;
}

test.describe("using this without a mouse", () => {
  /*
   * The sign-in form, which is the first thing anybody meets. Being unable to
   * reach the submit button here means being unable to use the application at
   * all, which is the worst version of this failure.
   */
  /*
   * Signed out on purpose, unlike everything else here.
   *
   * The rest of these reuse one session, because the server rate-limits
   * sign-in attempts. This one is about the form itself, so it needs a
   * browser that has not been through it.
   */
  test.describe("from a signed-out browser", () => {
    test.use({ storageState: { cookies: [], origins: [] } });

    test("signing in is possible from the keyboard alone", async ({ page }) => {
      await page.goto("/signin");

      /*
       * Waited for, not assumed. Typing into a form the application has not
       * finished taking over goes to the DOM and not to the model, which cost
       * an hour of thinking the keyboard was broken when the first two
       * characters of a username silently vanished.
       */
      await page.getByRole("button", { name: "Sign in", exact: true }).waitFor();

      /*
       * The username field already has focus — the form autofocuses it, which
       * is right for a page whose only purpose is that form. So the first
       * thing to do is type, not Tab.
       */
      await expect(page.getByLabel("Username or email")).toBeFocused();

      await page.keyboard.type("founder");
      await page.keyboard.press("Tab");
      await page.keyboard.type("correct horse battery staple");
      // Enter from within the form, which is how anybody submits one.
      await page.keyboard.press("Enter");

      await page.waitForURL((url) => !url.pathname.startsWith("/signin"), { timeout: 15_000 });
      expect(page.url()).not.toContain("/signin");
    });
  });

  /*
   * Focus has to be visible as well as present.
   *
   * Somebody tabbing through an interface that gives no sign of where they are
   * is navigating blind, which is why WCAG asks for this specifically.
   */
  test("what has focus is visible", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: /Search/ }).waitFor();
    await page.keyboard.press("Tab");

    const visible = await page.evaluate(() => {
      const el = document.activeElement;
      if (!el || el === document.body) return null;

      const styles = getComputedStyle(el);

      // Any of the three ways an interface can show focus. Requiring one in
      // particular would fail a design that chose another and still works.
      return {
        outline: styles.outlineStyle !== "none" && styles.outlineWidth !== "0px",
        ring: styles.boxShadow !== "none",
        border: styles.borderColor,
      };
    });

    expect(visible, "nothing took focus on the first Tab").not.toBeNull();
    expect(
      visible!.outline || visible!.ring,
      "the focused control shows no outline or ring, so somebody tabbing cannot see where they are",
    ).toBe(true);
  });

  /*
   * Tab must not get stuck.
   *
   * A trap is worse than an unreachable control: somebody who tabs into one
   * cannot leave without a mouse, which is precisely the thing they may not
   * have.
   */
  test("tab does not become trapped", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: /Search/ }).waitFor();
    const reached = await reachableByTab(page);

    // A handful of distinct places at least, or something is holding on.
    expect(new Set(reached).size, `tab reached: ${reached.join(", ")}`).toBeGreaterThan(4);
  });

  /*
   * The main things somebody does are all reachable.
   *
   * Named individually rather than counted, because a count passes while the
   * one control somebody needs is the one that is missing.
   */
  test("the controls that matter can all be reached", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: /Search/ }).waitFor();
    const reached = await reachableByTab(page);
    const found = (what: string) =>
      reached.some((label) => label.toLowerCase().includes(what.toLowerCase()));

    expect(found("search"), `tab reached: ${reached.join(", ")}`).toBe(true);
    expect(found("new channel"), `tab reached: ${reached.join(", ")}`).toBe(true);
    expect(found("notification"), `tab reached: ${reached.join(", ")}`).toBe(true);
    expect(found("settings"), `tab reached: ${reached.join(", ")}`).toBe(true);
  });

  /*
   * Somebody who has tabbed into a conversation can write in it.
   *
   * The composer is the point of the application, and reaching everything
   * except the thing people came for would be a strange kind of success.
   */
  test("the composer can be reached and typed into", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: /Search/ }).waitFor();
    await page
      .getByRole("link", { name: /General/ })
      .first()
      .click();
    await page.waitForURL(/\/rooms\//);

    const composer = page.getByPlaceholder(/Message/);
    await composer.focus();
    await page.keyboard.type("Nimefika hapa bila kipanya");

    await expect(composer).toHaveValue("Nimefika hapa bila kipanya");
  });
});
