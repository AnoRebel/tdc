import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * What makes this shareable, checked rather than assumed.
 *
 * A layer that quietly depends on one application's runtime is a layer with
 * one consumer and a misleading name. These are the properties that let a
 * second client — the desktop one, which has no fetch and no service worker —
 * extend it rather than fork it.
 */

const LAYER = join(import.meta.dirname, "..", "..");

/** Every source file in the layer, so a new one cannot slip past these. */
function sources(): { path: string; contents: string }[] {
  const found: { path: string; contents: string }[] = [];

  const walk = (directory: string) => {
    if (!existsSync(directory)) return;

    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const full = join(directory, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (/\.(ts|vue)$/.test(entry.name) && !entry.name.endsWith(".test.ts")) {
        found.push({ path: full.slice(LAYER.length + 1), contents: readFileSync(full, "utf8") });
      }
    }
  };

  walk(join(LAYER, "app"));
  return found;
}

describe("what the shared layer may depend on", () => {
  /*
   * The one thing that genuinely differs between the two clients.
   *
   * The web client sends over fetch and the desktop one over Wails bindings,
   * which have no URLs, headers or cookies. A file here that calls fetch has
   * made itself web-only without saying so, and the cost lands on whoever
   * tries to build the desktop client rather than on whoever wrote it.
   */
  it("never reaches for a transport itself", () => {
    const offenders = sources()
      .filter(({ contents }) => /\bfetch\s*\(|new XMLHttpRequest|\$fetch\s*\(/.test(contents))
      .map(({ path }) => path);

    expect(offenders, "these would only work in a browser").toEqual([]);
  });

  /*
   * Service workers do not exist in a Wails webview.
   *
   * Anything depending on one belongs in the web application, which is where
   * push notifications and the offline shell already live.
   */
  it("never depends on a service worker", () => {
    const offenders = sources()
      .filter(({ contents }) => /serviceWorker|PushManager/.test(contents))
      .map(({ path }) => path);

    expect(offenders, "these cannot work in a desktop webview").toEqual([]);
  });

  /*
   * The layer names itself, which is what makes #layers/ui resolve.
   *
   * Without it the alias is derived from a path, and every import in both
   * clients breaks the day this directory moves.
   */
  it("names itself so its own files can be imported", () => {
    const config = readFileSync(join(LAYER, "nuxt.config.ts"), "utf8");

    expect(config).toContain('name: "ui"');
  });
});
