import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

/**
 * Whether every locale can actually say what the interface says.
 *
 * The interface reads a key and gets whatever that locale holds. A key present
 * in one file and missing from another does not fail anywhere: the reader
 * falls back to English and the application looks translated to whoever added
 * the string, because they were reading English already.
 *
 * So the check is here, where adding a key to one file and not the other is
 * caught by whoever did it.
 */

const LOCALES = ["en", "sw"] as const;

function load(locale: string): Record<string, unknown> {
  const path = fileURLToPath(new URL(`../../locales/${locale}.json`, import.meta.url));
  return JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
}

/** Every leaf key, dotted, so two files can be compared as flat lists. */
function keysOf(value: unknown, prefix = ""): string[] {
  if (typeof value !== "object" || value === null) return [prefix];

  return Object.entries(value as Record<string, unknown>).flatMap(([key, nested]) =>
    keysOf(nested, prefix ? `${prefix}.${key}` : key),
  );
}

/** Every leaf value, so a translation can be checked for being one. */
function entriesOf(value: unknown, prefix = ""): [string, unknown][] {
  if (typeof value !== "object" || value === null) return [[prefix, value]];

  return Object.entries(value as Record<string, unknown>).flatMap(([key, nested]) =>
    entriesOf(nested, prefix ? `${prefix}.${key}` : key),
  );
}

describe("what every locale has to say", () => {
  const english = load("en");
  const englishKeys = keysOf(english).sort();

  it("has something to translate", () => {
    // A guard on the guard: an empty file would make every comparison below
    // pass while proving nothing.
    expect(englishKeys.length).toBeGreaterThan(50);
  });

  for (const locale of LOCALES.filter((one) => one !== "en")) {
    describe(locale, () => {
      const other = load(locale);

      it("says everything English says", () => {
        const missing = englishKeys.filter((key) => !keysOf(other).includes(key));
        expect(missing, `${locale}.json is missing these keys`).toEqual([]);
      });

      it("says nothing English does not", () => {
        /*
         * A key here and not in English is usually a rename that was applied to
         * one file: the old key stays behind, nothing reads it, and it looks
         * like a translation somebody did.
         */
        const extra = keysOf(other).filter((key) => !englishKeys.includes(key));
        expect(extra, `${locale}.json has keys English does not`).toEqual([]);
      });

      it("is actually translated", () => {
        /*
         * A value copied from English is not a translation, and it is what a
         * hurried pass leaves behind. Proper nouns and interface words that are
         * genuinely the same in both languages are the exception, so this
         * reports the count rather than each one: every string being identical
         * means the file was duplicated rather than translated.
         */
        const englishValues = new Map(entriesOf(english));
        const same = entriesOf(other).filter(
          ([key, value]) => typeof value === "string" && englishValues.get(key) === value,
        );

        expect(same.length).toBeLessThan(englishKeys.length / 2);
      });
    });
  }

  it("uses braces for anything it interpolates", () => {
    /*
     * nuxt-i18n-micro interpolates {name}. A string written with a different
     * convention — %s, {{ name }}, $name — renders the placeholder to the
     * reader instead of the value, and only in the locale that got it wrong.
     */
    for (const locale of LOCALES) {
      for (const [key, value] of entriesOf(load(locale))) {
        if (typeof value !== "string") continue;
        expect(
          value,
          `${locale}.json ${key} uses a placeholder style this does not interpolate`,
        ).not.toMatch(/%[sd]|\{\{|\$\{/);
      }
    }
  });

  it("agrees with English about what each string interpolates", () => {
    /*
     * A translation that drops a placeholder loses the value it was carrying —
     * "Welcome, Alice" becomes "Welcome" — and one that invents a placeholder
     * renders it literally, because nothing passes it.
     */
    const placeholders = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
    const englishValues = new Map(entriesOf(english));

    for (const locale of LOCALES.filter((one) => one !== "en")) {
      for (const [key, value] of entriesOf(load(locale))) {
        const source = englishValues.get(key);
        if (typeof value !== "string" || typeof source !== "string") continue;

        expect(placeholders(value), `${locale}.json ${key} interpolates something else`).toEqual(
          placeholders(source),
        );
      }
    }
  });
});
