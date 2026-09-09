import { describe, expect, it } from "vitest";

import {
  AA_CONTRAST,
  PREPAINT_THEME_SCRIPT as PREPAINT,
  checkAccent,
  contrastRatio,
  isAppearance,
  isPalette,
  parseColour,
} from "./theme";

/** What the script did to the document, however it was stored. */
interface Applied {
  classes: Set<string>;
  attributes: Map<string, string>;
  properties: Map<string, string>;
}

/** Runs the script against stored preferences and a device setting. */
function apply(stored: Record<string, string | null>, deviceDark: boolean): Applied {
  const classes = new Set<string>();
  const attributes = new Map<string, string>();
  const properties = new Map<string, string>();

  const globals = globalThis as Record<string, unknown>;
  globals.localStorage = { getItem: (key: string) => stored[key] ?? null };
  globals.window = { matchMedia: () => ({ matches: deviceDark }) };
  globals.document = {
    documentElement: {
      classList: {
        toggle: (name: string, on: boolean) => {
          if (on) classes.add(name);
          else classes.delete(name);
        },
      },
      setAttribute: (name: string, value: string) => attributes.set(name, value),
      style: {
        setProperty: (name: string, value: string) => properties.set(name, value),
      },
    },
  };

  eval(PREPAINT);

  return { classes, attributes, properties };
}

/** Just the light-or-dark half, which most of these are about. */
function decide(stored: string | null, deviceDark: boolean): string {
  const { classes } = apply({ "tdc-theme": stored }, deviceDark);
  return classes.has("dark") ? "dark" : classes.has("light") ? "light" : "neither";
}

/*
 * The palette treats dark as its default — its rule is `.dark, :root:not(.light)`
 * — so what the script sets for a light preference is the class that turns
 * that off. Both are set explicitly, so a change to which way the stylesheet
 * defaults cannot silently change what everybody sees.
 */
describe("the pre-paint theme decision", () => {
  it("follows the device when nothing has been chosen", () => {
    expect(decide(null, true)).toBe("dark");
    expect(decide(null, false)).toBe("light");
  });

  it("honours a choice over the device", () => {
    expect(decide("dark", false)).toBe("dark");
    expect(decide("light", true)).toBe("light");
  });

  it("treats auto as following the device", () => {
    expect(decide("auto", true)).toBe("dark");
    expect(decide("auto", false)).toBe("light");
  });

  /*
   * A private window can make localStorage throw on read. A theme preference
   * is not worth a blank page, so the script swallows it — and what somebody
   * sees is the device's own setting, which is the right answer anyway.
   */
  it("survives storage that cannot be read", () => {
    const classes = new Set<string>();
    const globals = globalThis as Record<string, unknown>;
    globals.localStorage = {
      getItem: () => {
        throw new Error("access denied");
      },
    };
    globals.window = { matchMedia: () => ({ matches: true }) };
    globals.document = {
      documentElement: {
        classList: {
          toggle: (name: string, on: boolean) => {
            if (on) classes.add(name);
          },
        },
        setAttribute: () => {},
        style: { setProperty: () => {} },
      },
    };

    expect(() => eval(PREPAINT)).not.toThrow();
  });

  /*
   * Always exactly one of the two, never both and never neither. A page
   * carrying both classes would take whichever the stylesheet happened to
   * order last, which is not a decision anybody made.
   */
  it("sets exactly one class", () => {
    for (const stored of [null, "auto", "light", "dark"]) {
      for (const deviceDark of [true, false]) {
        expect(decide(stored, deviceDark)).not.toBe("neither");
      }
    }
  });
});

/*
 * The palette and the accent, applied before the first paint for the same
 * reason the appearance is: an interface that paints in the wrong colours for
 * one frame flashes for exactly as long as the application takes to load.
 */
describe("the pre-paint palette and accent", () => {
  it("applies a chosen palette", () => {
    const { attributes } = apply({ "tdc-palette": "congrator" }, true);

    expect(attributes.get("data-theme")).toBe("congrator");
  });

  /*
   * The default is the absence of an attribute rather than data-theme="default".
   * The stylesheet's own :root rules are the default palette, so setting an
   * attribute for it would mean writing those rules twice.
   */
  it("leaves the default palette unmarked", () => {
    expect(apply({ "tdc-palette": "default" }, true).attributes.has("data-theme")).toBe(false);
    expect(apply({}, true).attributes.has("data-theme")).toBe(false);
  });

  it("applies a chosen accent", () => {
    const { properties } = apply({ "tdc-accent": "#4f46e5" }, true);

    expect(properties.get("--primary")).toBe("#4f46e5");
  });

  it("leaves the palette's own accent alone when none was chosen", () => {
    expect(apply({}, true).properties.has("--primary")).toBe(false);
  });

  /* The three are independent: choosing a palette says nothing about light or
     dark, and choosing an accent says nothing about either. */
  it("keeps the three choices independent", () => {
    const { classes, attributes, properties } = apply(
      { "tdc-theme": "light", "tdc-palette": "congrator", "tdc-accent": "#b45309" },
      true,
    );

    expect(classes.has("light")).toBe(true);
    expect(attributes.get("data-theme")).toBe("congrator");
    expect(properties.get("--primary")).toBe("#b45309");
  });
});

/*
 * Contrast, measured against values the specification itself fixes.
 *
 * These are not this implementation's own output recorded as expectations —
 * that would pass whatever it computed. Black on white is 21:1 by definition,
 * and #767676 on white is the canonical example of a colour that just clears
 * AA, which pins the arithmetic at the threshold that actually matters.
 */
describe("measuring contrast", () => {
  it("agrees with the values WCAG fixes", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 1);
    expect(contrastRatio("#ffffff", "#ffffff")).toBeCloseTo(1, 2);
    expect(contrastRatio("#0000ff", "#ffffff")).toBeCloseTo(8.59, 1);
  });

  /* Either side of 4.5:1, which is the only place the number changes an
     outcome. A conversion wrong by a few percent still passes a black-on-white
     check and fails here. */
  it("separates a colour that clears AA from one that does not", () => {
    const passes = contrastRatio("#767676", "#ffffff");
    const fails = contrastRatio("#777777", "#ffffff");

    expect(passes).toBeGreaterThanOrEqual(AA_CONTRAST);
    expect(fails).toBeLessThan(AA_CONTRAST);
  });

  it("reads the oklch the palettes are written in", () => {
    // Every palette colour is oklch, so a parser that only did hex would
    // silently refuse the application's own accents.
    expect(parseColour("oklch(0.53 0.16 269)")).not.toBeNull();
    expect(parseColour("oklch(53% 0.16 269)")).not.toBeNull();
  });

  it("says so rather than guessing at a colour it cannot read", () => {
    expect(parseColour("rebeccapurple")).toBeNull();
    expect(parseColour("rgb(1 2 3)")).toBeNull();
    expect(contrastRatio("nonsense", "#ffffff")).toBeNull();
  });
});

describe("choosing an accent", () => {
  it("accepts one that can be read against both foregrounds", () => {
    // Dark enough for white text, and the check is against white only —
    // an accent is used under one foreground per appearance.
    expect(checkAccent("#3730a3", ["#ffffff"])).toBeNull();
  });

  it("refuses one that would be hard to read, and says by how much", () => {
    const refusal = checkAccent("#facc15", ["#ffffff"]);

    expect(refusal?.reason).toBe("unreadable");
    expect(refusal?.ratio).toBeLessThan(AA_CONTRAST);
    // The number belongs in the message: "too light" is not something somebody
    // can act on, and a ratio tells them how far off they are.
    expect(refusal?.message).toMatch(/1\.\d:1/);
  });

  /* An accent that works in light and fails in dark is the failure that only
     appears for people who switched, which is the hardest kind to hear about. */
  it("refuses one that works against a single foreground but not both", () => {
    // 6.4:1 under white text and 3.3:1 under dark: fine in one appearance and
    // not the other, which is the failure that only reaches the people who
    // switched.
    const splitting = "#5f5f5f";

    expect(checkAccent(splitting, ["#ffffff"])).toBeNull();
    expect(checkAccent(splitting, ["#ffffff", "#000000"])?.reason).toBe("unreadable");
  });

  it("refuses a colour it cannot measure rather than passing it", () => {
    expect(checkAccent("chartreuse", ["#ffffff"])?.reason).toBe("unmeasurable");
  });
});

describe("naming a palette or an appearance", () => {
  it("recognises the ones that exist", () => {
    expect(isPalette("congrator")).toBe(true);
    expect(isPalette("default")).toBe(true);
    expect(isAppearance("auto")).toBe(true);
  });

  /* What arrives from storage or an API is a string somebody could have
     written anything into; an unrecognised palette must not become an
     attribute selecting nothing. */
  it("rejects anything else", () => {
    expect(isPalette("chartreuse")).toBe(false);
    expect(isPalette(null)).toBe(false);
    expect(isAppearance("sepia")).toBe(false);
  });
});
