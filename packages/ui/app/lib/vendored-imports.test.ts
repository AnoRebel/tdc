import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * A vendored component used in a template has to be imported.
 *
 * The scanner registers these with their directory as a prefix — Button.vue
 * under ui/button becomes <UiButton> — so a template writing <Button> resolves
 * to nothing. What makes that worth a check rather than a convention is that it
 * type-checks perfectly: vue-tsc knows UiButton exists globally and has no
 * opinion about the name actually written, so the first sign of the mistake is
 * a control missing from a screen at runtime.
 *
 * Nearly shipped exactly that while moving the desktop's appearance screen over.
 */

const ROOTS = [
  join(import.meta.dirname, "..", ".."),
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "web", "app"),
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "desktop", "frontend", "app"),
];

const VENDORED = join(import.meta.dirname, "..", "components", "ui");

/** The component names under components/ui, as a template would write them. */
function vendoredNames(): string[] {
  const names: string[] = [];
  for (const entry of readdirSync(VENDORED)) {
    const full = join(VENDORED, entry);
    if (!statSync(full).isDirectory()) continue;
    for (const file of readdirSync(full)) {
      if (file.endsWith(".vue")) names.push(file.replace(/\.vue$/, ""));
    }
  }
  return names;
}

function templates(): { path: string; contents: string }[] {
  const found: { path: string; contents: string }[] = [];
  const walk = (directory: string, root: string) => {
    if (!existsSync(directory)) return;
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
      const full = join(directory, entry.name);
      if (entry.isDirectory()) walk(full, root);
      else if (entry.name.endsWith(".vue")) {
        found.push({ path: full.slice(root.length + 1), contents: readFileSync(full, "utf8") });
      }
    }
  };
  for (const root of ROOTS) walk(root, root);
  return found;
}

describe("vendored components", () => {
  it("are imported wherever they are rendered", () => {
    const names = vendoredNames();
    const offenders: string[] = [];

    for (const { path, contents } of templates()) {
      // The component's own file renders itself; nothing to import there.
      if (path.includes(join("components", "ui"))) continue;

      const template = contents.match(/<template>([\s\S]*)<\/template>/)?.[1] ?? "";
      if (!template) continue;

      for (const name of names) {
        const rendered = new RegExp(`<${name}[\\s/>]`).test(template);
        if (!rendered) continue;

        const imported =
          contents.includes(`components/ui/${name.toLowerCase()}`) ||
          new RegExp(`import\\s*\\{[^}]*\\b${name}\\b[^}]*\\}`).test(contents);

        if (!imported) offenders.push(`${path}: <${name}> is rendered but never imported`);
      }
    }

    expect(
      offenders.sort(),
      "the scanner registers these with a Ui prefix, so an unimported name resolves to nothing",
    ).toEqual([]);
  });
});
