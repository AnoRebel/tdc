import { existsSync, readdirSync, readFileSync, realpathSync } from "node:fs";
import { dirname, join } from "node:path";

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
 * Counted over what the application can reach — the web and desktop clients
 * and the packages they share — rather than over everything installed. The
 * documentation site (apps/docs) is a separate application built on its own
 * component library, which brings its own table; that copy never meets these.
 */

const ROOT = join(import.meta.dirname, "..", "..", "..", "..");
const APPLICATION = ["apps/web", "apps/desktop/frontend", "packages/ui", "packages/types"];

/**
 * Every package the application's workspaces resolve, directly or through
 * another package. Bun's isolated layout links a package's dependencies beside
 * it, in the node_modules directory that holds it, so following those links
 * from each workspace walks the whole tree it can load.
 */
function reachable(): Map<string, Set<string>> {
  const versions = new Map<string, Set<string>>();
  const visited = new Set<string>();
  const queue = APPLICATION.map((workspace) => join(ROOT, workspace, "node_modules"));

  while (queue.length) {
    const modules = queue.pop()!;
    if (visited.has(modules) || !existsSync(modules)) continue;
    visited.add(modules);

    for (const entry of readdirSync(modules)) {
      if (entry.startsWith(".")) continue;
      const names = entry.startsWith("@")
        ? readdirSync(join(modules, entry)).map((name) => `${entry}/${name}`)
        : [entry];

      for (const name of names) {
        let directory: string;
        try {
          directory = realpathSync(join(modules, name));
        } catch {
          continue;
        }
        const manifest = join(directory, "package.json");
        if (!existsSync(manifest)) continue;
        const { version } = JSON.parse(readFileSync(manifest, "utf8")) as { version?: string };
        if (!versions.has(name)) versions.set(name, new Set());
        if (version) versions.get(name)!.add(version);
        // Its dependencies are its siblings.
        queue.push(name.includes("/") ? dirname(dirname(directory)) : dirname(directory));
      }
    }
  }
  return versions;
}

const tree = reachable();

/** Every version of a package the application can load. */
function versionsOf(name: string): string[] {
  return [...(tree.get(name) ?? [])];
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
   *
   * The way this breaks in practice is a VueUse upgrade. reka-ui — which every
   * vendored shadcn component is built on — and @vuepic/vue-datepicker both
   * require @vueuse/core ^14, so raising the catalog to 15 does not replace
   * VueUse: it resolves 15 alongside 14, and 14 brings its own Vue. The
   * catalog is held at 14 for that reason, and should move only once reka-ui
   * accepts 15.
   */
  it("resolves one copy of Vue", () => {
    const versions = versionsOf("vue");

    expect(versions.length, `found ${versions.join(", ")}`).toBe(1);
  });
});
