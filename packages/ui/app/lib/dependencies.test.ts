import { readdirSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * One copy of the things that only work as one copy.
 *
 * A table library carries its own state and its own idea of what a column is.
 * Two copies in a tree do not fail loudly — they each work, separately, and
 * a table built by one is unreadable to the other. The symptom is a component
 * that renders nothing with no error, which is the most expensive kind of
 * failure to find.
 *
 * Bun's isolated layout puts every resolved version under node_modules/.bun,
 * so counting the directories counts the copies.
 */

const MODULES = join(import.meta.dirname, "..", "..", "..", "..", "node_modules", ".bun");

/** Every version of a package that the workspace resolved. */
function versionsOf(name: string): string[] {
  const prefix = `${name.replace("/", "+")}@`;

  return (
    readdirSync(MODULES)
      .filter((entry) => entry.startsWith(prefix))
      // A directory name carries the version and sometimes a hash of the peers
      // it was resolved against; the version is what matters here.
      .map((entry) => entry.slice(prefix.length).split("+")[0]!)
  );
}

describe("packages that must exist once", () => {
  /*
   * The table's own core, which vue-table wraps.
   *
   * Two copies would mean two registries of column definitions, and a table
   * configured through one rendering as empty through the other.
   */
  it("resolves one copy of the table core", () => {
    const versions = versionsOf("@tanstack/table-core");

    expect(versions.length, `found ${versions.join(", ")}`).toBe(1);
  });

  /*
   * The store both the table and its Vue bindings read through.
   *
   * A second copy means state written by one and never seen by the other,
   * which shows up as a table that does not react to anything.
   */
  it("resolves one copy of the store", () => {
    const versions = versionsOf("@tanstack/store");

    expect(versions.length, `found ${versions.join(", ")}`).toBe(1);
  });

  /*
   * Vue itself, which is the same failure with a wider blast radius: two
   * copies means two reactivity systems, and components from one that simply
   * do not update inside the other.
   */
  it("resolves one copy of Vue", () => {
    const versions = versionsOf("vue");

    expect(versions.length, `found ${versions.join(", ")}`).toBe(1);
  });
});
