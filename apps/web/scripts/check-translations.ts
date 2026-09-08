#!/usr/bin/env bun
/**
 * Whether every string this application shows can be translated.
 *
 * The translation files are checked for parity by their own tests. This asks
 * the other question: whether the interface is actually asking for
 * translations, or has quietly gone back to writing English into components
 * where no other language can reach it.
 *
 * Reported rather than enforced. A hard failure here would block work on a
 * screen somebody is midway through, and the number going up is the signal —
 * it is written down so a rise is visible in a diff rather than discovered a
 * year later.
 */

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const ROOTS = ["app", "../../packages/ui/app"];

/** Every component and page, which is where interface strings live. */
function templates(directory: string): string[] {
  const found: string[] = [];

  const walk = (path: string) => {
    for (const entry of readdirSync(path)) {
      const full = join(path, entry);
      if (statSync(full).isDirectory()) walk(full);
      else if (entry.endsWith(".vue")) found.push(full);
    }
  };

  try {
    walk(directory);
  } catch {
    // A root that is not there yet, which is not an error.
  }

  return found;
}

/**
 * Text a person would read, sitting directly in a template.
 *
 * Deliberately rough: this counts what is worth looking at rather than
 * deciding what is wrong. A precise rule would need to know that "Ctrl K" is a
 * key name and "Cancel" is a word, which is a judgement rather than a pattern.
 */
function untranslatedIn(contents: string): string[] {
  const template = contents.slice(contents.indexOf("<template>"));

  return (
    [...template.matchAll(/>\s*([A-Z][A-Za-z ,.'’—-]{6,60})\s*</g)]
      .map((match) => match[1]!.trim())
      // Anything already going through a translation call is fine.
      .filter((text) => !text.includes("{{"))
      .filter((text) => !/^[A-Z][a-z]+ [A-Z]$/.test(text))
  );
}

function main() {
  const files = ROOTS.flatMap(templates);

  let total = 0;
  const worst: { file: string; count: number; sample: string[] }[] = [];

  for (const file of files) {
    const strings = untranslatedIn(readFileSync(file, "utf8"));
    if (!strings.length) continue;

    total += strings.length;
    worst.push({ file, count: strings.length, sample: strings.slice(0, 3) });
  }

  worst.sort((a, b) => b.count - a.count);

  console.log(`${total} literal strings across ${worst.length} of ${files.length} templates.`);
  console.log("\nMost, first:");
  for (const entry of worst.slice(0, 8)) {
    console.log(`  ${entry.count.toString().padStart(3)}  ${entry.file}`);
    console.log(`       ${entry.sample.join(" · ")}`);
  }
  console.log(
    "\nEach of these is a sentence only English speakers can read. Moving one\n" +
      "into locales/en.json and calling $t() makes it translatable.",
  );
}

main();
