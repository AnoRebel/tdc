import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * What the form components exist to make impossible.
 *
 * They were added late, over markup that had been written by hand on every
 * screen — 35 inputs, 6 text areas, 4 selects — each repeating the label, the
 * invalid state and the description that ties an error to the field it is
 * about. Repetition is where those get dropped, and dropping them is invisible
 * until somebody uses the application without seeing it.
 *
 * These read the components rather than mounting them: the rules are about what
 * the source guarantees, and a check that needed a browser would not run on
 * every save.
 */

const HERE = import.meta.dirname;
const read = (...parts: string[]) => readFileSync(join(HERE, ...parts), "utf8");

describe("the button", () => {
  const source = read("button", "Button.vue");

  /*
   * The failure this replaced: Edit and Delete on a message carried title="…"
   * and nothing else. A tooltip is for a pointer; a screen reader is told
   * nothing, and the button is announced as "button".
   */
  it("gives its label to assistive technology, not only to a tooltip", () => {
    expect(source, "a label that only becomes a title is a tooltip, not a name").toMatch(
      /:aria-label="props\.label/,
    );
    expect(source).toMatch(/:title="props\.label/);
  });

  /*
   * And a caller names it through the prop, not the attribute.
   *
   * The component binds aria-label from its own prop, so an aria-label passed
   * as an attribute is overridden by the prop's absence and the button ends up
   * with no name at all. That is exactly what happened while moving the
   * controls over: six files kept their aria-label, and the appearance toggle
   * became a button nothing could find — four tests failed on a control that
   * had simply lost its name.
   */
  it("is named through the prop rather than the attribute", async () => {
    const { existsSync, readdirSync, readFileSync } = await import("node:fs");
    const { join } = await import("node:path");

    const roots = [
      join(HERE, "..", ".."),
      join(HERE, "..", "..", "..", "..", "apps", "web", "app"),
      join(HERE, "..", "..", "..", "..", "apps", "desktop", "frontend", "app"),
    ];

    const offenders: string[] = [];
    const walk = (directory: string, root: string) => {
      if (!existsSync(directory)) return;
      for (const entry of readdirSync(directory, { withFileTypes: true })) {
        if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
        const full = join(directory, entry.name);
        if (entry.isDirectory()) {
          walk(full, root);
          continue;
        }
        if (!entry.name.endsWith(".vue")) continue;
        if (full.includes(join("components", "ui", "button"))) continue;

        const contents = readFileSync(full, "utf8");
        for (const tag of contents.matchAll(/<Button\b[^>]*?>/gs)) {
          if (/aria-label=/.test(tag[0])) {
            offenders.push(full.slice(root.length + 1));
            break;
          }
        }
      }
    };
    for (const root of roots) walk(root, root);

    expect(
      offenders.sort(),
      "the component binds aria-label from its label prop, so an attribute here is overridden — use label",
    ).toEqual([]);
  });

  it("dims and refuses a press when disabled", () => {
    expect(source).toMatch(/disabled:opacity-50/);
    expect(source).toMatch(/disabled:cursor-not-allowed/);
  });

  /*
   * Every colour a token. A literal here would survive a palette change and
   * leave one control the wrong colour in a theme nobody tested.
   */
  it("takes every colour from a theme token", () => {
    const literals = source.match(/#[0-9a-fA-F]{3,8}\b|\boklch\(|\brgb\(/g) ?? [];
    expect(literals, "a colour written here cannot follow the palette").toEqual([]);
  });
});

describe("the fields", () => {
  for (const [name, file] of [
    ["input", join("input", "Input.vue")],
    ["textarea", join("textarea", "Textarea.vue")],
  ] as const) {
    const source = read(file);

    it(`${name} says when what it holds was refused`, () => {
      expect(source, "aria-invalid is how a refusal is announced").toMatch(/aria-invalid/);
      expect(source, "and the message has to be pointed at").toMatch(/aria-describedby/);
    });

    it(`${name} takes every colour from a theme token`, () => {
      const literals = source.match(/#[0-9a-fA-F]{3,8}\b|\boklch\(|\brgb\(/g) ?? [];
      expect(literals).toEqual([]);
    });
  }

  /*
   * A label that points at nothing is decoration: clicking it moves no focus,
   * and a screen reader does not connect it to the control it sits above.
   */
  it("a label is tied to a control rather than floating above one", () => {
    const source = read("label", "Label.vue");
    expect(source).toMatch(/for: string/);
    expect(source).toMatch(/:for="props\.for"/);
  });

  /*
   * The wiring nobody remembers, done once: the label points at the control,
   * the control points at the error, and the error announces itself.
   */
  it("a field ties its label, its control and its error together", () => {
    const source = read("field", "Field.vue");
    expect(source, "the error must be announced when it appears").toMatch(/role="alert"/);
    expect(source, "and the control must name it").toMatch(/described-by="describedBy"/);
    expect(source, "and the label must point at the control").toMatch(/:for="id"/);
  });
});

/*
 * The choice list is drawn rather than native, and that is the point: a native
 * one is drawn by the platform, so the same screen is a GTK control in the
 * desktop webview and a Chrome control in a browser.
 */
describe("the choice list", () => {
  const source = read("select", "Select.vue");

  it("is drawn here, so both clients show the same control", () => {
    expect(source).toMatch(/from "reka-ui"/);

    /*
     * The template only. The prose above it says the word "<select>" while
     * explaining why there is not one, which a check on the whole file reads as
     * the very thing it is forbidding.
     */
    const template = source.match(/<template>([\s\S]*)<\/template>/)?.[1] ?? "";
    expect(template, "a native control would be drawn by the platform").not.toMatch(/<select[\s>]/);
  });

  it("takes every colour from a theme token", () => {
    const literals = source.match(/#[0-9a-fA-F]{3,8}\b|\boklch\(|\brgb\(/g) ?? [];
    expect(literals).toEqual([]);
  });
});

/*
 * And the whole directory follows the palette.
 *
 * Checked across every vendored component rather than the new ones alone: a
 * colour written into any of them is one a theme cannot reach, and the point of
 * vendoring was to own them, not to freeze them.
 */
describe("every vendored component", () => {
  function componentFiles(directory: string): string[] {
    const found: string[] = [];
    for (const entry of readdirSync(directory)) {
      const full = join(directory, entry);
      if (statSync(full).isDirectory()) found.push(...componentFiles(full));
      else if (entry.endsWith(".vue")) found.push(full);
    }
    return found;
  }

  it("writes no colour a palette cannot change", () => {
    const offenders: string[] = [];

    for (const file of componentFiles(HERE)) {
      const source = readFileSync(file, "utf8");

      /*
       * A colour computed per person is the one honest exception: the avatar
       * picks a hue from a name so two people are told apart, and no fixed
       * token can do that. It is still held to a lightness that clears 4.5:1
       * against its own text, which is the property that actually matters.
       */
      const computedFromAName = /oklch\([^)]*\$\{/.test(source);

      const literals = (source.match(/#[0-9a-fA-F]{6}\b|\boklch\([^)]*\)/g) ?? []).filter(
        (colour) => !(computedFromAName && colour.startsWith("oklch(")),
      );
      if (literals.length) offenders.push(`${file.slice(HERE.length + 1)}: ${literals.join(", ")}`);
    }

    expect(offenders.sort(), "these colours are fixed, whatever the theme says").toEqual([]);
  });
});

/*
 * The first field of a sign-in screen takes focus.
 *
 * A screen whose only purpose is one form should not ask somebody to click
 * before typing. This was lost when the controls moved onto components — the
 * attribute was on the markup, the component did not have the prop, and it
 * simply vanished. Nothing type-checked differently; the keyboard test noticed,
 * eventually, by which point four other tests were failing for reasons that
 * looked unrelated.
 */
describe("a screen that exists to be typed into", () => {
  it("puts the cursor in the first field", async () => {
    const { readFileSync } = await import("node:fs");
    const { join } = await import("node:path");

    /*
     * The repository root, found rather than counted.
     *
     * Counting "../" from a nested directory is how this test spent two runs
     * looking for the applications inside packages/.
     */
    const REPO = join(HERE, "..", "..", "..", "..", "..");

    const forms = [
      join(REPO, "apps", "web", "app", "pages", "signin.vue"),
      join(REPO, "apps", "web", "app", "pages", "register.vue"),
      join(REPO, "apps", "web", "app", "pages", "setup.vue"),
      join(REPO, "apps", "desktop", "frontend", "app", "pages", "signin.vue"),
    ];

    const offenders: string[] = [];
    for (const path of forms) {
      const contents = readFileSync(path, "utf8");
      if (!/autofocus/.test(contents)) offenders.push(path.split("/").slice(-3).join("/"));
    }

    expect(offenders.sort(), "somebody arriving here has to click before they can type").toEqual(
      [],
    );
  });
});
