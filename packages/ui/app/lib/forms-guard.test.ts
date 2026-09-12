import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * A form with rules states them once, rather than checking by hand.
 *
 * The threshold is deliberately not "every <form>". A dialog with one box and
 * a submit disabled until it is non-empty gains nothing from a schema and a
 * field API — the rule is "somebody typed something", which the disabled
 * button already says, and wrapping it would be ceremony rather than clarity.
 *
 * What does earn it is more than one field, because that is where the rules
 * start being written out at each control, the error messages drift from the
 * checks, and whether the whole thing may be submitted becomes a condition
 * assembled by hand from several refs. Sign-in, registration and first-run
 * setup were all in that state.
 */

const ROOTS = [
  join(import.meta.dirname, "..", ".."),
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "web", "app"),
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "desktop", "frontend", "app"),
];

/** A control somebody types a value into, as these templates write them. */
const FIELD = /<(Input|Textarea|Select)[\s/>]/g;

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

describe("forms with rules", () => {
  it("use the form library rather than a handful of refs", () => {
    const offenders: string[] = [];

    for (const { path, contents } of templates()) {
      const template = contents.match(/<template>([\s\S]*)<\/template>/)?.[1] ?? "";
      if (!/<form[\s>]/.test(template)) continue;
      if (contents.includes("useForm")) continue;

      const fields = [...template.matchAll(FIELD)].length;
      if (fields > 1) {
        offenders.push(`${path}: ${fields} fields, submitted by hand`);
      }
    }

    expect(
      offenders.sort(),
      "more than one field is where hand-written rules start to drift from the " +
        "messages beside them — state them once, in a schema",
    ).toEqual([]);
  });

  /*
   * And a schema is Valibot, not something hand-rolled beside it.
   *
   * Only where a form library is already in use: this is about which validator
   * those forms reach for, not about adding one where none is wanted.
   */
  it("validates with Valibot where they validate at all", () => {
    const offenders: string[] = [];

    for (const { path, contents } of templates()) {
      if (!contents.includes("useForm")) continue;
      if (!contents.includes("validators:")) continue;
      if (!contents.includes('from "valibot"')) {
        offenders.push(`${path}: validates without a Valibot schema`);
      }
    }

    expect(offenders.sort()).toEqual([]);
  });
});
