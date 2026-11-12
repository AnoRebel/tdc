import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * Middleware asks for what it needs before it waits for anything.
 *
 * A composable reaches for the Nuxt context to inject from, and that context is
 * only reliably present in the synchronous part of a middleware. Call one after
 * an await and it injects from nothing: useAuth() throws "no API client has
 * been provided" from a client that provided one correctly at startup.
 *
 * This is not hypothetical. The desktop client's own auth middleware asked Go
 * which server was current — an await — and only then called useAuth, so
 * signing in succeeded and the navigation that followed it failed, reporting a
 * missing transport. The web's middleware calls useAuth on its first line and
 * never showed the problem, which is exactly how it survived to be found here.
 */

/*
 * This client's own middleware, and the shared layer's — which runs here just
 * the same, and is where a new one is most likely to be written by somebody
 * looking only at the web.
 */
const MIDDLEWARE = [
  import.meta.dirname,
  join(import.meta.dirname, "..", "..", "..", "..", "..", "packages", "ui", "app", "middleware"),
];

describe("route middleware", () => {
  it("calls its composables before it awaits", () => {
    const offenders: string[] = [];

    for (const directory of MIDDLEWARE) {
      if (!existsSync(directory)) continue;

      for (const name of readdirSync(directory)) {
        if (!name.endsWith(".ts") || name.endsWith(".test.ts")) continue;

        const contents = readFileSync(join(directory, name), "utf8");

        /*
         * The first await inside the middleware body, which is indented by
         * two. Anything deeper sits in a nested function and runs later, on its
         * own terms, rather than suspending the middleware itself.
         */
        const firstAwait = contents.search(/^ {2}(?:await |(?:const|let) [^=]*= await )/m);
        if (firstAwait < 0) continue;

        for (const match of contents
          .slice(firstAwait)
          .matchAll(/^ {2}(?:(?:const|let)\s.*?=\s*)?\b(use[A-Z][A-Za-z]*)\s*\(/gm)) {
          const name2 = match[1];
          if (!name2 || name2 === "useState") continue;
          offenders.push(`${name}: ${name2}()`);
        }
      }
    }

    expect(
      offenders.sort(),
      "these run after an await, where there is no Nuxt context to inject " +
        "from — move them above the first await",
    ).toEqual([]);
  });
});
