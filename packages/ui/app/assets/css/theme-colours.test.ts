import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { compile } from "tailwindcss";
import { describe, expect, it } from "vitest";

/**
 * Every colour utility the interface uses has a colour behind it.
 *
 * Tailwind says nothing about a class it does not know: bg-card, with no
 * --color-card in the theme, compiles to no CSS at all, and a dialog drawn with
 * it is see-through over the page. That shipped once, in ten components. This
 * compiles every colour-shaped class written in the layer and the applications
 * against the real theme (main.css) and fails on any that comes out empty.
 */

const here = dirname(fileURLToPath(import.meta.url));
const ui = join(here, "../../..");
const roots = [
  join(ui, "app"),
  join(ui, "../../apps/web/app"),
  join(ui, "../../apps/desktop/frontend/app"),
];

const PREFIX =
  "(?:bg|text|border(?:-[xytrblse])?|ring|ring-offset|outline|divide|fill|stroke|from|via|to|placeholder|caret|accent|decoration|shadow|inset-shadow|inset-ring)";
// A colour-shaped utility after any variants: bg-card, hover:text-primary/80, dark:border-border.
// Not inside brackets, where transition-[border-color] names a CSS property.
const CANDIDATE = new RegExp(
  `(?<![\\w$/.,[-])((?:[a-z0-9-]+:)*)(${PREFIX}-[a-z][a-z0-9-]*(?:/\\d+)?)(?![\\w/.-])`,
  "g",
);

/** Strings shaped like colour utilities that are something else. */
const NOT_UTILITIES = new Set([
  "ring-pulse", // a class with its own rules, in CallIncoming.vue
  "ring-ended", // the tdc:ring-ended event
  "accent-refusal", // an element id on the settings page
  "stroke-width", // SVG attributes the charts set
  "stroke-linejoin",
]);

function files(dir: string): string[] {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  return entries.flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === "node_modules" ? [] : files(path);
    return /\.(vue|ts)$/.test(entry.name) && !entry.name.endsWith(".test.ts") ? [path] : [];
  });
}

/** The class lists in a file: template attributes and the strings scripts build them from. */
function candidates(source: string): string[] {
  const found: string[] = [];
  for (const literal of source.matchAll(/(["'`])((?:(?!\1)[^\\\n]|\\.)*)\1/g)) {
    for (const match of literal[2]!.matchAll(CANDIDATE))
      if (!NOT_UTILITIES.has(match[2]!)) found.push(match[2]!);
  }
  return found;
}

/** Whether the CSS has a rule for the class: bg-card/50 is written .bg-card\/50. */
function hasRule(css: string, name: string): boolean {
  const escaped = name.replace(/[^a-zA-Z0-9_-]/g, (char) => `\\${char}`);
  const source = `\\.${escaped.replace(/\\/g, "\\\\")}(?![\\w\\\\-])`;
  return new RegExp(source).test(css);
}

async function theme() {
  const require = createRequire(import.meta.url);
  // Only the theme matters here: the other stylesheets main.css imports add no colours.
  const css = readFileSync(join(here, "main.css"), "utf8")
    .split("\n")
    .filter((line) => !/^@(import|source|plugin)\b/.test(line) || line.includes('"tailwindcss"'))
    .join("\n");
  return compile(css, {
    base: here,
    loadStylesheet: async (id: string, base: string) => {
      const path = require.resolve(id === "tailwindcss" ? "tailwindcss/index.css" : id, {
        paths: [base],
      });
      return { path, base: dirname(path), content: readFileSync(path, "utf8") };
    },
  });
}

describe("colour utilities", () => {
  it("all have a colour in the theme", async () => {
    const used = new Map<string, Set<string>>();
    for (const root of roots) {
      for (const file of files(root)) {
        for (const name of candidates(readFileSync(file, "utf8"))) {
          if (!used.has(name)) used.set(name, new Set());
          used.get(name)!.add(file.slice(file.indexOf("/", ui.length - 12) + 1));
        }
      }
    }
    expect(used.size).toBeGreaterThan(50);

    const compiler = await theme();
    const output = compiler.build([...used.keys()]);
    const empty = [...used.entries()]
      .filter(([name]) => !hasRule(output, name))
      .map(([name, where]) => `${name}  (${[...where].join(", ")})`);

    expect(empty).toEqual([]);
  });
});
