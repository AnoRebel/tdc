import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { AA_CONTRAST, MATERIALS, contrastRatio } from "#layers/ui/app/lib/theme";

/**
 * Every material is readable in this interface, in light and in dark.
 *
 * Crystal Neuo checks its own pairs. This checks the pairs this interface
 * actually draws — its text on its grounds, its labels on its buttons — after
 * the mapping in scripts/build-materials.ts, which is where a role could be
 * put somewhere the design system never measured it.
 */

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(here, "materials.generated.css"), "utf8").replace(
  /\/\*[\s\S]*?\*\//g,
  "",
);

/** The variables a rule sets, by the first selector it names. */
function blocks(): Map<string, Record<string, string>> {
  const found = new Map<string, Record<string, string>>();
  for (const [, selectors, body] of css.matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    const first = selectors!.trim().split(",")[0]!.trim();
    const values: Record<string, string> = {};
    for (const [, name, value] of body!.matchAll(/(--[a-z0-9-]+):\s*([^;]+);/g)) {
      // A person's own accent comes first; the material's is the fallback.
      values[name!] = value!.replace(/^var\(--accent-(?:light|dark),\s*(.+)\)$/, "$1").trim();
    }
    found.set(first, values);
  }
  return found;
}

const rules = blocks();

/** Light is the material's own rule; dark adds to it. */
function palette(material: string, theme: "light" | "dark") {
  const light = rules.get(`html[data-theme="${material}"]`);
  const dark = rules.get(`.dark[data-theme="${material}"]`);
  if (!light || !dark) throw new Error(`no rules for ${material}`);
  return theme === "light" ? light : { ...light, ...dark };
}

const TEXT: [string, string][] = [
  ["--foreground", "--background"],
  ["--foreground", "--surface"],
  ["--foreground", "--surface-raised"],
  ["--foreground", "--muted"],
  ["--muted-foreground", "--background"],
  ["--muted-foreground", "--surface"],
  ["--primary-foreground", "--primary"],
  ["--accent-foreground", "--accent"],
  ["--destructive", "--background"],
  ["--destructive-foreground", "--destructive"],
  ["--success-text", "--background"],
];

/** Boundaries and focus need 3:1, which WCAG asks of anything that is not text. */
const NON_TEXT: [string, string][] = [
  ["--ring", "--background"],
  ["--border-strong", "--background"],
];

describe.each(
  MATERIALS.flatMap((material) => [[material, "light"] as const, [material, "dark"] as const]),
)("%s in %s", (material, theme) => {
  const colours = palette(material, theme);

  it.each(TEXT)("%s on %s is readable", (text, ground) => {
    const ratio = contrastRatio(colours[text]!, colours[ground]!);
    expect(ratio, `${colours[text]} on ${colours[ground]}`).not.toBeNull();
    expect(ratio!).toBeGreaterThanOrEqual(AA_CONTRAST);
  });

  it.each(NON_TEXT)("%s on %s can be seen", (mark, ground) => {
    const ratio = contrastRatio(colours[mark]!, colours[ground]!);
    expect(ratio!).toBeGreaterThanOrEqual(3);
  });
});

describe("high contrast", () => {
  const colours = [...rules].find(([selector]) =>
    selector.startsWith(':root[data-contrast="more"]'),
  )?.[1] as Record<string, string>;

  it("is there, after every material, so it wins over whichever is chosen", () => {
    expect(colours).toBeDefined();
    expect(css.lastIndexOf('data-contrast="more"')).toBeGreaterThan(
      css.lastIndexOf('[data-theme="hybrid"]'),
    );
  });

  it.each(TEXT)("%s on %s reaches 7:1", (text, ground) => {
    expect(contrastRatio(colours[text]!, colours[ground]!)!).toBeGreaterThanOrEqual(7);
  });
});
