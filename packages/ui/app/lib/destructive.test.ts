import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * Something irreversible asks first, and says what will be lost.
 *
 * Not a style rule. Ending a session signs a device out mid-sentence and tells
 * whoever is holding it nothing; suspending somebody removes their access;
 * withdrawing an invitation makes a link somebody was sent stop working. Each
 * is the right power to have and the wrong thing to do by a misplaced press on
 * a list where every row looks alike.
 *
 * Ending a session was doing exactly that — the button called the API directly
 * while every other destructive action on the same screen asked — which is why
 * this is a check rather than a convention.
 */

const ROOTS = [
  join(import.meta.dirname, "..", ".."),
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "web", "app"),
  join(import.meta.dirname, "..", "..", "..", "..", "apps", "desktop", "frontend", "app"),
];

/*
 * The handlers that must not be reachable straight from a press.
 *
 * Named rather than inferred: "delete" appears all over a codebase — a Map, a
 * cache, an upload being cancelled — and a check that matched the word would
 * report those and be switched off within a week.
 */
const IRREVERSIBLE = [
  "endSession",
  "confirmRemoval",
  "confirmWithdrawal",
  "confirmSuspension",
  "confirmDelete",
  "blockSender",
  "confirmUnblock",
  "confirmEnding",
];

/** A handler that is safe to reach directly: it opens the question. */
const ASKS = /^(confirm|ask)/;

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

describe("something irreversible", () => {
  it("is never one press away", () => {
    const offenders: string[] = [];

    for (const { path, contents } of templates()) {
      const template = contents.match(/<template>([\s\S]*)<\/template>/)?.[1] ?? "";
      if (!template) continue;

      for (const handler of IRREVERSIBLE) {
        // A handler whose own name says it is the answer to a question is the
        // thing a dialog calls, not the thing a list row calls.
        if (ASKS.test(handler)) continue;

        const pressed = new RegExp(`@click="${handler}\\(`).test(template);
        if (!pressed) continue;

        // Unless the press is inside the dialog that asked.
        const insideDialog = new RegExp(`<ConfirmDialog[^>]*@confirm="${handler}\\(`, "s").test(
          template,
        );

        if (!insideDialog) offenders.push(`${path}: ${handler}() runs on a press`);
      }
    }

    expect(
      offenders.sort(),
      "these do something that cannot be undone without asking first",
    ).toEqual([]);
  });

  /*
   * And the asking says what will be lost.
   *
   * "Are you sure?" is not a question anybody can answer — it asks somebody to
   * confirm a thing they have not been told. Every dialog carries a detail
   * saying what happens, which is the standard the earlier ones set.
   */
  it("says what will happen, rather than only asking whether to continue", () => {
    const offenders: string[] = [];

    for (const { path, contents } of templates()) {
      const template = contents.match(/<template>([\s\S]*)<\/template>/)?.[1] ?? "";
      for (const dialog of template.matchAll(/<ConfirmDialog[\s\S]*?\/>/g)) {
        const markup = dialog[0];
        if (!/(?::detail=|detail=)/.test(markup)) {
          const title = markup.match(/title="([^"]*)"/)?.[1] ?? "a dialog";
          offenders.push(`${path}: "${title}" asks without saying what happens`);
        }
      }
    }

    expect(offenders.sort()).toEqual([]);
  });
});
