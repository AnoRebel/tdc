import { describe, expect, it } from "vitest";

import { PREPAINT_THEME_SCRIPT as PREPAINT } from "./theme";

/** Runs the script against a stored preference and a device setting. */
function decide(stored: string | null, deviceDark: boolean): string {
  const classes = new Set<string>();

  const globals = globalThis as Record<string, unknown>;
  globals.localStorage = { getItem: () => stored };
  globals.window = { matchMedia: () => ({ matches: deviceDark }) };
  globals.document = {
    documentElement: {
      classList: {
        toggle: (name: string, on: boolean) => {
          if (on) classes.add(name);
          else classes.delete(name);
        },
      },
    },
  };

  eval(PREPAINT);

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
