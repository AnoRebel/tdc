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

/** The worker browsers actually receive. */
function builtWorker(): string {
  return readFileSync(WORKER, "utf8");
}

function shippedDenylist(): RegExp[] {
  const worker = builtWorker();

  /*
   * Matched loosely on purpose.
   *
   * The build emits this minified or not depending on the strategy, and an
   * earlier version of this test keyed on the minified spacing — so switching
   * to a written service worker broke the test rather than reporting anything
   * about the worker. What matters is the list, not its whitespace.
   *
   * The closing bracket is found by scanning rather than by a lazy match: the
   * patterns contain "]" inside a character class, which is what the first
   * attempt tripped over.
   */
  const start = worker.search(/denylist\s*:\s*\[/);
  if (start < 0) throw new Error("the built service worker carries no denylist");

  const from = worker.indexOf("[", start);
  let depth = 0;
  let inRegex = false;

  for (let i = from; i < worker.length; i++) {
    const character = worker[i];

    if (character === "\\") {
      i++;
      continue;
    }
    if (character === "/" && worker[i - 1] !== "*") inRegex = !inRegex;
    if (inRegex) continue;

    if (character === "[") depth++;
    if (character === "]") {
      depth--;
      if (depth === 0) {
        return eval(worker.slice(from, i + 1)) as RegExp[];
      }
    }
  }

  throw new Error("the denylist in the built service worker is not closed");
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

/*
 * Attachments are served from the cache once they have been fetched.
 *
 * This is the difference between scrolling back through a conversation on a
 * bus and seeing a column of broken images. Asserted against the built worker
 * rather than the source, because a rule that did not survive the build is a
 * rule that is not running.
 */
describe.skipIf(!existsSync(WORKER))("the built worker's runtime caching", () => {
  it("keeps attachments", () => {
    const worker = builtWorker();

    expect(worker).toContain("tdc-attachments");
    // Cache first, not network first: an attachment is addressed by an
    // identifier that never names different bytes, so a cached copy is the
    // same file rather than a stale one.
    expect(worker).toContain("CacheFirst");
  });

  /*
   * Only successful responses are kept.
   *
   * Without this an authentication failure or a 404 is cached as though it
   * were the file, and the image stays broken after the cause is fixed —
   * including across a reconnection, which is when somebody would expect it to
   * come back.
   */
  it("does not keep failures", () => {
    expect(builtWorker()).toContain("CacheableResponsePlugin");
  });

  /*
   * The cache is bounded.
   *
   * A phone's storage is not, and a hundred large images is more than somebody
   * bargained for when they installed a chat application.
   */
  it("bounds what it keeps", () => {
    expect(builtWorker()).toContain("ExpirationPlugin");
  });

  /*
   * A query string does not change which attachment this is.
   *
   * Found the hard way: entries stored under a URL carrying anything extra —
   * a cache-buster, a parameter a future version appends — were never matched
   * again. The cache filled and served nothing, so an attachment appeared to
   * be kept and was missing offline, which is the worst of both.
   */
  it("matches an attachment regardless of its query string", () => {
    /*
     * Scoped to this route's own options.
     *
     * Searching the whole worker found "ignoreSearch" in workbox's bundled
     * code — a comment and an internal helper — so the assertion held with
     * the option removed. A test that cannot fail is worse than no test,
     * because it is counted.
     */
    expect(attachmentRoute()).toContain("ignoreSearch");
  });

  /*
   * Matched on the path, not on what kind of request the browser called it.
   *
   * Requiring request.destination to be "image" excluded most of the ways an
   * attachment is really fetched: a plain fetch has no destination at all, and
   * neither does a prefetch. Only the ones loaded through an <img> were kept.
   */
  it("does not require a request destination", () => {
    expect(attachmentRouteCondition()).not.toContain("destination");
  });
});

/**
 * The attachment strategy's own configuration, from the built worker.
 *
 * Scoped rather than searched whole: workbox's bundled code mentions most of
 * these names itself, and an assertion against the whole file passes whether
 * or not this route carries the option.
 */
function attachmentRoute(): string {
  const worker = builtWorker();

  const start = worker.indexOf('cacheName: "tdc-attachments"');
  if (start < 0) throw new Error("the built worker has no attachment cache");

  return worker.slice(start, start + 600);
}

/** What decides whether a request is an attachment, in the built worker. */
function attachmentRouteCondition(): string {
  const worker = builtWorker();

  const cache = worker.indexOf('cacheName: "tdc-attachments"');
  if (cache < 0) throw new Error("the built worker has no attachment cache");

  // The registerRoute call immediately preceding it, which carries the match.
  const route = worker.lastIndexOf("registerRoute(", cache);
  return worker.slice(route, cache);
}
