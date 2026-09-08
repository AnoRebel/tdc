#!/usr/bin/env bun
/**
 * What a browser must fetch before this application is usable.
 *
 * A budget rather than a measurement: the number only means something if
 * exceeding it stops the build. Payload growth is never one decision anybody
 * would defend — it is thirty small ones nobody noticed, and this is the thing
 * that makes somebody notice.
 *
 * Measured gzipped, because that is what actually crosses the wire, and from
 * the built output rather than a bundler's report — the report is what a tool
 * believes and the output is what people download.
 */

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

const OUTPUT = ".output/public";
const SHELL = join(OUTPUT, "index.html");

/*
 * The budgets, in kilobytes of gzipped transfer.
 *
 * Blocking is what the browser must have before it can render anything —
 * somebody on a slow connection watches a blank page for exactly this long.
 * Prefetched is fetched at idle priority for pages nobody has opened yet: it
 * costs data but not waiting, so it gets its own, looser, limit.
 *
 * Set above today's figures with room to work in, and low enough that a
 * careless import is caught. Raising one should be a decision somebody makes
 * on purpose, in a commit that says why.
 */
const BUDGETS = {
  blocking: 175,
  prefetched: 70,
};

/** Gzipped size in kilobytes, or zero for something that is not there. */
function transferred(path: string): number {
  const full = join(OUTPUT, path.replace(/^\//, ""));
  if (!existsSync(full)) return 0;

  return gzipSync(readFileSync(full), { level: 9 }).length / 1024;
}

function matchAll(html: string, pattern: RegExp): string[] {
  return [...html.matchAll(pattern)].map((match) => match[1]!);
}

function main() {
  if (!existsSync(SHELL)) {
    console.error(`No build to measure at ${SHELL}. Run the build first.`);
    process.exit(1);
  }

  const html = readFileSync(SHELL, "utf8");

  /*
   * Everything the shell needs before it can run: the stylesheet, the entry
   * script, and the modules preloaded alongside it.
   */
  const blocking = new Set([
    ...matchAll(html, /rel="modulepreload"[^>]*href="([^"]+)"/g),
    ...matchAll(html, /rel="stylesheet"[^>]*href="([^"]+)"/g),
    ...matchAll(html, /<script[^>]*src="([^"]+)"/g),
  ]);

  // Asked for on the side, for pages nobody has opened yet.
  const prefetched = new Set(matchAll(html, /rel="prefetch"[^>]*href="([^"]+)"/g));

  if (blocking.size === 0) {
    console.error("Found nothing blocking in the shell, which cannot be right.");
    process.exit(1);
  }

  const shellSize = gzipSync(Buffer.from(html), { level: 9 }).length / 1024;
  const sizes = {
    blocking: [...blocking].reduce((total, path) => total + transferred(path), shellSize),
    prefetched: [...prefetched].reduce((total, path) => total + transferred(path), 0),
  };

  let exceeded = false;

  for (const [what, budget] of Object.entries(BUDGETS)) {
    const actual = sizes[what as keyof typeof sizes];
    const within = actual <= budget;
    if (!within) exceeded = true;

    const headroom = budget - actual;
    console.log(
      `${within ? "within" : "OVER  "}  ${what.padEnd(11)} ` +
        `${actual.toFixed(1).padStart(6)} KB of ${budget} KB ` +
        `(${headroom >= 0 ? `${headroom.toFixed(1)} KB to spare` : `${(-headroom).toFixed(1)} KB over`})`,
    );
  }

  if (exceeded) {
    console.error(
      "\nThe initial load is over budget.\n\n" +
        "Something new is being downloaded before the application can start.\n" +
        "Usually that is an import at the top of a file that only one page\n" +
        "needs — moving it to a dynamic import inside that page keeps it out\n" +
        "of everybody else's first visit.\n\n" +
        "If the growth is deliberate, raise the budget in this script and say\n" +
        "in the commit what was added and why it belongs in the first load.",
    );
    process.exit(1);
  }

  // The largest few, so somebody watching the number knows where it lives.
  const largest = [...blocking]
    .map((path) => ({ path, size: transferred(path) }))
    .sort((a, b) => b.size - a.size)
    .slice(0, 3);

  console.log(
    `\nLargest blocking: ${largest
      .map((entry) => `${entry.path.split("/").pop()} ${entry.size.toFixed(1)} KB`)
      .join(", ")}`,
  );
}

main();
