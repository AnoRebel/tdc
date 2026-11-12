import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * The translations, held to the things that go quietly wrong.
 *
 * A missing key is invisible until somebody reading in that language meets it,
 * and by then it is a bug report saying "the app showed me a full stop". These
 * catch it at the point somebody adds a string instead.
 */

const LOCALES = join(import.meta.dirname);

function load(code: string): Record<string, unknown> {
  return JSON.parse(readFileSync(join(LOCALES, `${code}.json`), "utf8")) as Record<string, unknown>;
}

/** Every key, flattened, so two files can be compared as sets. */
function keysOf(object: Record<string, unknown>, prefix = ""): Set<string> {
  const keys = new Set<string>();

  for (const [key, value] of Object.entries(object)) {
    const path = `${prefix}${key}`;

    if (typeof value === "object" && value !== null) {
      for (const nested of keysOf(value as Record<string, unknown>, `${path}.`)) {
        keys.add(nested);
      }
    } else {
      keys.add(path);
    }
  }

  return keys;
}

/** The placeholders a string expects, such as {room} or {when}. */
function placeholdersIn(value: string): Set<string> {
  return new Set([...value.matchAll(/\{(\w+)\}/g)].map((match) => match[1]!));
}

function stringsOf(object: Record<string, unknown>, prefix = ""): Map<string, string> {
  const strings = new Map<string, string>();

  for (const [key, value] of Object.entries(object)) {
    const path = `${prefix}${key}`;

    if (typeof value === "string") strings.set(path, value);
    else if (typeof value === "object" && value !== null) {
      for (const [nested, text] of stringsOf(value as Record<string, unknown>, `${path}.`)) {
        strings.set(nested, text);
      }
    }
  }

  return strings;
}

const english = load("en");
const swahili = load("sw");

describe("the translations", () => {
  /*
   * Every language says everything.
   *
   * English is the fallback, so a missing Swahili key shows English rather
   * than breaking — which is the right behaviour and also the reason nobody
   * notices. This is what notices.
   */
  it("says the same things in both languages", () => {
    const inEnglish = keysOf(english);
    const inSwahili = keysOf(swahili);

    const untranslated = [...inEnglish].filter((key) => !inSwahili.has(key));
    const orphaned = [...inSwahili].filter((key) => !inEnglish.has(key));

    expect(untranslated, "these would silently show English").toEqual([]);
    // A key with no English is one nothing can fall back to, and usually a
    // rename that only happened in one file.
    expect(orphaned, "these are translated but no longer used").toEqual([]);
  });

  /*
   * A placeholder dropped in translation is a sentence with a hole in it.
   *
   * "Message {room}" translated without {room} renders as "Andika kwenye" —
   * grammatical, and missing the only part that said which conversation.
   */
  it("keeps every placeholder a string depends on", () => {
    const inEnglish = stringsOf(english);
    const inSwahili = stringsOf(swahili);

    const wrong: string[] = [];

    for (const [key, text] of inEnglish) {
      const translated = inSwahili.get(key);
      if (!translated) continue;

      const expected = placeholdersIn(text);
      const actual = placeholdersIn(translated);

      const missing = [...expected].filter((name) => !actual.has(name));
      const unknown = [...actual].filter((name) => !expected.has(name));

      if (missing.length || unknown.length) {
        wrong.push(
          `${key}: missing ${missing.join(", ") || "none"}, unknown ${unknown.join(", ") || "none"}`,
        );
      }
    }

    expect(wrong).toEqual([]);
  });

  /*
   * Nothing is left as an empty string.
   *
   * An empty translation is worse than a missing one: a missing key falls back
   * to English, and an empty one renders as nothing at all.
   */
  it("has no empty strings", () => {
    for (const [code, file] of [
      ["en", english],
      ["sw", swahili],
    ] as const) {
      const empty = [...stringsOf(file)].filter(([, text]) => !text.trim()).map(([key]) => key);

      expect(empty, `${code} has keys that would render as nothing`).toEqual([]);
    }
  });

  /*
   * Swahili is not English wearing a different filename.
   *
   * A copied file passes every check above while translating nothing. Proper
   * nouns and the product name are the same in both, which is why this allows
   * a few rather than none.
   */
  it("is actually translated", () => {
    const inEnglish = stringsOf(english);
    const inSwahili = stringsOf(swahili);

    const identical = [...inEnglish].filter(([key, text]) => inSwahili.get(key) === text);

    expect(
      identical.length,
      `${identical.length} of ${inEnglish.size} strings are identical: ${identical
        .slice(0, 5)
        .map(([key]) => key)
        .join(", ")}`,
    ).toBeLessThan(inEnglish.size / 10);
  });
});
