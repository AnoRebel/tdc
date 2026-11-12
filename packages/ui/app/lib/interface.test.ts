// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { beforeEach, describe, expect, it, vi } from "vitest";

import { PREPAINT_INTERFACE_SCRIPT, resolveMotion } from "./interface";

describe("resolving the motion level", () => {
  it("uses an explicit choice whatever the system says", () => {
    for (const choice of ["full", "reduced", "off"] as const) {
      expect(resolveMotion(choice, true)).toBe(choice);
      expect(resolveMotion(choice, false)).toBe(choice);
    }
  });

  it("follows the system otherwise, reducing rather than stopping", () => {
    expect(resolveMotion("system", false)).toBe("full");
    expect(resolveMotion("system", true)).toBe("reduced");
    expect(resolveMotion(null, true)).toBe("reduced");
    expect(resolveMotion("sparkly", false)).toBe("full");
  });
});

/*
 * The head script, run as the browser runs it.
 *
 * It is a string, so nothing type-checks it; running it is the only way to
 * know it does what the composable would have done.
 */
describe("deciding motion before the first paint", () => {
  function systemReduces(reduce: boolean) {
    vi.stubGlobal("matchMedia", (media: string) => ({
      matches: media.includes("prefers-reduced-motion") && reduce,
      media,
    }));
  }

  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute("data-motion");
    document.documentElement.removeAttribute("data-density");
  });

  it("applies a stored choice", () => {
    systemReduces(true);
    localStorage.setItem("tdc-motion", "full");
    localStorage.setItem("tdc-density", "compact");
    eval(PREPAINT_INTERFACE_SCRIPT);

    expect(document.documentElement.getAttribute("data-motion")).toBe("full");
    expect(document.documentElement.getAttribute("data-density")).toBe("compact");
  });

  it("follows the system when nothing is stored", () => {
    systemReduces(true);
    eval(PREPAINT_INTERFACE_SCRIPT);
    expect(document.documentElement.getAttribute("data-motion")).toBe("reduced");

    systemReduces(false);
    eval(PREPAINT_INTERFACE_SCRIPT);
    expect(document.documentElement.getAttribute("data-motion")).toBe("full");
  });

  it("survives storage that refuses to be read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(() => eval(PREPAINT_INTERFACE_SCRIPT)).not.toThrow();
    vi.restoreAllMocks();
  });
});

/*
 * "Off" has to mean off.
 *
 * The switch is only as good as the stylesheet's discipline: a looping
 * animation added later without a data-motion selector would keep running for
 * somebody who turned motion off. So every rule that starts an animation in the
 * motion stylesheet is checked for one.
 */
describe("the motion stylesheet", () => {
  const css = readFileSync(
    join(import.meta.dirname, "..", "assets", "css", "motion.css"),
    "utf8",
  ).replace(/\/\*[\s\S]*?\*\//g, "");

  it("only starts an animation under a motion level that allows it", () => {
    const rules = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)];
    const offenders = rules
      .filter(([, , body]) => /(^|;|\s)animation:\s*(?!none)/.test(body!))
      .map(([, selector]) => selector!.trim())
      .filter((selector) => !selector.includes("data-motion"));

    expect(offenders).toEqual([]);
  });

  it("stops everything when motion is off", () => {
    expect(css).toMatch(/:root\[data-motion="off"\] \*/);
    expect(css).toContain("transition-duration: 0.01ms !important");
    expect(css).toContain("animation-iteration-count: 1 !important");
  });

  it("falls back to the system setting before the preference is known", () => {
    expect(css).toMatch(/prefers-reduced-motion: reduce\)\s*\{\s*:root:not\(\[data-motion\]\)/);
  });
});
