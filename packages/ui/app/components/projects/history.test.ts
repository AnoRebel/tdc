import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * Every kind of event a project's own history can hold is said in words.
 *
 * The kinds are read from the server's source, where they are declared, rather
 * than listed here: a kind added there and not here would be shown as its raw
 * name — "project.something" — which is a history nobody can read.
 */

const EVENTS = join(
  import.meta.dirname,
  "..",
  "..",
  "..",
  "..",
  "..",
  "server",
  "internal",
  "events",
);

function projectEventKinds(): string[] {
  const kinds = new Set<string>();
  for (const file of readdirSync(EVENTS)) {
    if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
    const source = readFileSync(join(EVENTS, file), "utf8");
    for (const match of source.matchAll(/"(project\.[a-z_]+)"/g)) kinds.add(match[1]!);
  }
  return [...kinds];
}

describe("a project's history", () => {
  const component = readFileSync(join(import.meta.dirname, "ProjectHistory.vue"), "utf8");

  it("finds the kinds where the server declares them", () => {
    expect(projectEventKinds()).toContain("project.created");
  });

  it.each(projectEventKinds())("describes %s", (kind) => {
    expect(component).toContain(`case "${kind}":`);
  });
});
