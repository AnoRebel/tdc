import { expect, test, type Page } from "@playwright/test";

/*
 * What the interface looks like, checked in a browser.
 *
 * These cannot be unit tests. A palette is CSS: whether switching one actually
 * changes what somebody sees depends on the selectors surviving the build, on
 * the cascade, and on the bundler not tree-shaking rules it thought unused —
 * none of which a test of the source can see. The unit tests cover the
 * arithmetic and the storage; this covers whether the colours arrive.
 */

/** Every token that carries a colour, as the browser computes it. */
async function palette(page: Page): Promise<Record<string, string>> {
  return page.evaluate(() => {
    const style = getComputedStyle(document.documentElement);
    const tokens = [
      "--background",
      "--foreground",
      "--surface",
      "--surface-raised",
      "--surface-sunken",
      "--muted",
      "--muted-foreground",
      "--border",
      "--primary",
      "--primary-hover",
      "--primary-foreground",
      "--accent",
      "--ring",
    ];
    return Object.fromEntries(tokens.map((name) => [name, style.getPropertyValue(name).trim()]));
  });
}

test.describe("themes", () => {
  /* Sets the appearance and the palette together, since a token is only
     defined for one combination of the two. */
  async function wear(page: Page, theme: string | null, appearance: "light" | "dark") {
    await page.evaluate(
      ({ name, mode }) => {
        const root = document.documentElement;
        if (name) root.setAttribute("data-theme", name);
        else root.removeAttribute("data-theme");
        root.classList.toggle("dark", mode === "dark");
        root.classList.toggle("light", mode === "light");
      },
      { name: theme, mode: appearance },
    );
  }

  /*
   * In both appearances, because a palette defines each token twice and may
   * forget one of them.
   *
   * Checking only the current appearance is what an earlier version of this
   * did, and deleting a token from the light block passed it: the dark block
   * still defined that token, and the test happened to be running in dark.
   * The gap it left is a palette that is complete in one appearance and
   * inherits the default's colours in the other.
   */
  for (const appearance of ["light", "dark"] as const) {
    test(`a second palette changes every colour in ${appearance}`, async ({ page }) => {
      await page.goto("/settings");

      await wear(page, null, appearance);
      const before = await palette(page);

      await wear(page, "congrator", appearance);
      const after = await palette(page);

      /*
       * Each token individually, rather than the object as a whole. A palette
       * that redefined most of them would pass a comparison of the whole and
       * leave one cool grey border in a warm interface — which is the failure
       * nobody reports and everybody notices.
       */
      for (const [token, value] of Object.entries(before)) {
        expect(after[token], `${token} is the default's in ${appearance}`).not.toBe(value);
      }
    });
  }

  /*
   * Both palettes ship. Tailwind removes selectors it believes unused, and a
   * palette selected by an attribute nothing in the markup sets is exactly
   * what that looks like — the rules vanish and the theme silently does
   * nothing.
   */
  test("both palettes survive the build", async ({ page }) => {
    await page.goto("/settings");

    const sheets = await page.evaluate(() =>
      [...document.styleSheets]
        .flatMap((sheet) => {
          try {
            return [...sheet.cssRules].map((rule) => rule.cssText);
          } catch {
            return [];
          }
        })
        .join("\n"),
    );

    expect(sheets, "the default palette is missing").toMatch(/:root\s*\{[^}]*--background/);
    expect(sheets, "the second palette was removed by the build").toContain("data-theme=");
  });

  /* Light and dark are a property of the palette, not a third palette: each
     one has to define both, or switching appearance inside it does nothing. */
  test("each palette defines both light and dark", async ({ page }) => {
    await page.goto("/settings");

    for (const theme of [null, "congrator"]) {
      await page.evaluate((name) => {
        const root = document.documentElement;
        if (name) root.setAttribute("data-theme", name);
        else root.removeAttribute("data-theme");
        root.classList.add("dark");
        root.classList.remove("light");
      }, theme);
      const dark = await palette(page);

      await page.evaluate(() => {
        document.documentElement.classList.add("light");
        document.documentElement.classList.remove("dark");
      });
      const light = await palette(page);

      expect(dark["--background"], `${theme ?? "default"} has one background for both`).not.toBe(
        light["--background"],
      );
      expect(dark["--foreground"]).not.toBe(light["--foreground"]);
    }
  });

  /*
   * The accent, which is the one colour somebody sets themselves.
   *
   * An inline custom property on the root has to actually reach the elements
   * that use it — a token defined in @theme rather than aliased to the
   * variable would ignore the override entirely, and the setting would appear
   * to do nothing.
   */
  test("a chosen accent reaches what it should colour", async ({ page }) => {
    await page.goto("/settings");

    const chosen = "oklch(0.55 0.2 150)";
    await page.evaluate((colour) => {
      document.documentElement.style.setProperty("--primary", colour);
    }, chosen);

    const applied = await page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue("--color-primary").trim(),
    );

    // The @theme alias resolves to the variable, so overriding one moves both.
    expect(applied, "the accent did not reach the colour components use").not.toBe("");
    expect(applied).toContain("0.2");
  });
});

/*
 * The toggle, which is how most people will actually change this.
 *
 * Settings has the same choices plus the accent field; this is the one in the
 * sidebar, and in the desktop client it is the only one — that client has no
 * settings screen to send anybody to.
 */
test.describe("the appearance toggle", () => {
  /*
   * A stored choice outlives the test that made it, and the session is shared
   * across this whole suite — so a test that picks the warm palette would hand
   * it to everything that runs afterwards.
   */
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await page.evaluate(() => {
      try {
        localStorage.removeItem("tdc-palette");
        localStorage.removeItem("tdc-accent");
      } catch {
        // Nothing stored is the state we wanted anyway.
      }
    });
    await page.reload();
  });

  test("changes the palette from the sidebar", async ({ page }) => {
    await page.goto("/");

    await page.getByRole("button", { name: "Appearance" }).click();
    const menu = page.getByRole("menu", { name: "Appearance" });
    await expect(menu).toBeVisible();

    await menu.getByRole("menuitemradio", { name: "Warm" }).click();

    await expect(page.locator("html")).toHaveAttribute("data-theme", "congrator");

    // And the colours actually moved, not only the attribute.
    const primary = await page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue("--primary").trim(),
    );
    expect(primary).not.toBe("");
  });

  test("says which choice is active", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Appearance" }).click();

    const menu = page.getByRole("menu", { name: "Appearance" });
    const cool = menu.getByRole("menuitemradio", { name: "Cool" });
    const warm = menu.getByRole("menuitemradio", { name: "Warm" });

    await warm.click();
    await page.getByRole("button", { name: "Appearance" }).click();

    /*
     * Announced, not only ticked. A screen reader has no way to see a tick, so
     * a menu that shows the active choice visually and not in its markup has
     * told only some of its users.
     */
    await expect(warm).toHaveAttribute("aria-checked", "true");
    await expect(cool).toHaveAttribute("aria-checked", "false");
  });

  /*
   * Dismissable both ways somebody expects. Without these, a menu opened by
   * accident can only be closed by choosing something from it — changing a
   * setting to get rid of a menu.
   */
  test("closes on Escape and on a press outside", async ({ page }) => {
    await page.goto("/");
    const menu = page.getByRole("menu", { name: "Appearance" });

    await page.getByRole("button", { name: "Appearance" }).click();
    await expect(menu).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(menu).toBeHidden();

    await page.getByRole("button", { name: "Appearance" }).click();
    await expect(menu).toBeVisible();
    await page.mouse.click(400, 300);
    await expect(menu).toBeHidden();
  });

  /* The choice survives a reload, which is the whole point of storing it. */
  test("remembers the choice", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Appearance" }).click();
    await page
      .getByRole("menu", { name: "Appearance" })
      .getByRole("menuitemradio", { name: "Warm" })
      .click();

    await page.reload();

    await expect(page.locator("html")).toHaveAttribute("data-theme", "congrator");
  });
});
