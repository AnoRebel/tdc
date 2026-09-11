import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * Navigation goes through the locale-aware route, not a raw path.
 *
 * The application runs with `no_prefix` today, so "/signin" resolves and every
 * raw path works. That is exactly what makes this worth checking: nothing fails
 * now, and the day the strategy changes to one that prefixes the locale, every
 * one of them quietly resolves to the default locale — or to nothing — and the
 * breakage is spread across two applications rather than in one place.
 *
 * The module's own guidance is to prefer a named route over a path, because a
 * path has to be matched and a name does not.
 */

const ROOTS = [
  join(import.meta.dirname, "..", ".."), // this layer
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "web", "app"),
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "desktop", "frontend", "app"),
];

/** Every page, component, composable and middleware in the applications. */
function sources(): { path: string; contents: string }[] {
  const found: { path: string; contents: string }[] = [];

  const walk = (directory: string, root: string) => {
    if (!existsSync(directory)) return;
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
      const full = join(directory, entry.name);
      if (entry.isDirectory()) walk(full, root);
      else if (/\.(ts|vue)$/.test(entry.name) && !entry.name.endsWith(".test.ts")) {
        found.push({ path: full.slice(root.length + 1), contents: readFileSync(full, "utf8") });
      }
    }
  };

  for (const root of ROOTS) walk(root, root);
  return found;
}

describe("how the applications navigate", () => {
  it("uses a named locale route rather than a raw path", () => {
    const offenders: string[] = [];

    for (const { path, contents } of sources()) {
      // navigateTo("/x"), router.push("/x"), router.replace("/x")
      for (const match of contents.matchAll(
        /(navigateTo|router\.push|router\.replace)\(\s*["'](\/[^"']*)["']/g,
      )) {
        offenders.push(`${path}: ${match[1]}("${match[2]}")`);
      }

      // navigateTo({ path: "/x" }) and router.push({ path: `/x/${id}` }) alike.
      // The template-literal form is how a conversation's own URL was built,
      // which a check for quoted strings alone walked straight past.
      for (const match of contents.matchAll(
        /(navigateTo|router\.push|router\.replace)\(\s*\{\s*path:\s*[`"'](\/[^`"']*)/g,
      )) {
        offenders.push(`${path}: ${match[1]}({ path: "${match[2]}..." })`);
      }

      // router.push(`/x/${id}`) — the same thing without the object.
      for (const match of contents.matchAll(
        /(navigateTo|router\.push|router\.replace)\(\s*`(\/[^`]*)`/g,
      )) {
        offenders.push(`${path}: ${match[1]}(\`${match[2]}\`)`);
      }

      /*
       * A literal `to="/x"` on a link. The bound form — :to="localeRoute(...)"
       * — is what this is steering towards, and a dynamic path built from an
       * identifier is left alone: a conversation's own URL is not a named route.
       */
      for (const match of contents.matchAll(/(?<![:\w])to=["'](\/[a-z][a-z-]*)["']/g)) {
        offenders.push(`${path}: to="${match[1]}"`);
      }
    }

    expect(
      offenders.sort(),
      "these navigate by raw path, which resolves only while the locale strategy " +
        'adds no prefix — use the named route: localeRoute({ name: "signin" })',
    ).toEqual([]);
  });
});
