import { existsSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

/*
 * What the service worker must never answer from its cache.
 *
 * Read from the built worker rather than the config, because the config is
 * what was meant and the worker is what browsers receive. A build step that
 * dropped or mangled the list would leave the config looking correct.
 *
 * Skipped when there is no build, so a checkout without one is not a failure —
 * but the assertions below run in CI, where the build precedes the tests.
 */
const WORKER = "./.output/public/sw.js";

function shippedDenylist(): RegExp[] {
  const worker = readFileSync(WORKER, "utf8");

  // To the closing bracket followed by "})" — a plain "]" would stop inside a
  // character class, which is exactly what the first attempt did.
  const match = worker.match(/denylist:\[(.*?)\]\}\)/);
  if (!match) throw new Error("the built service worker carries no denylist");

  // eslint-disable-next-line no-eval -- evaluating regexes the build emitted
  return eval(`[${match[1]}]`) as RegExp[];
}

/** Whether a path would be answered from the cached application shell. */
function servedFromShell(denylist: RegExp[], path: string): boolean {
  return !denylist.some((pattern) => pattern.test(path));
}

describe.skipIf(!existsSync(WORKER))("what the cached shell may answer", () => {
  /*
   * The rule this whole task turns on.
   *
   * A single-page application tells its service worker to answer any
   * navigation with the shell, which is what makes a deep link work offline.
   * Unqualified, that swallows the API too — and a live connection answered
   * with a cached document does not fail in a way anything reports. It simply
   * never connects, and the application looks merely quiet.
   */
  it("never answers the live connection from the cache", () => {
    expect(servedFromShell(shippedDenylist(), "/api/live")).toBe(false);
  });

  it("never answers anything the server owns from the cache", () => {
    const denylist = shippedDenylist();

    for (const path of [
      "/api/rooms/abc/timeline",
      "/api/server",
      "/api/attachments/xyz",
      "/api/admin/audit",
    ]) {
      expect(servedFromShell(denylist, path), path).toBe(false);
    }
  });

  /*
   * A file answered with the shell is a corrupt download rather than an error,
   * which is harder to diagnose than a failure.
   */
  it("never answers a file request from the cache", () => {
    const denylist = shippedDenylist();

    for (const path of ["/icon-192.png", "/_nuxt/entry.a1b2.js", "/manifest.webmanifest"]) {
      expect(servedFromShell(denylist, path), path).toBe(false);
    }
  });

  /*
   * And the other half: the shell has to answer the application's own routes,
   * or a deep link opened offline is a blank page. A denylist that denied
   * everything would pass the checks above and break the feature.
   */
  it("still answers the application's own pages from the cache", () => {
    const denylist = shippedDenylist();

    for (const path of ["/", "/rooms/abc", "/settings", "/people", "/audit"]) {
      expect(servedFromShell(denylist, path), path).toBe(true);
    }
  });
});
