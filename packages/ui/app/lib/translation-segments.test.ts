import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { replace, split, translatable } from "./translation-segments";

/*
 * The same cases the server's segmenter is tested against. A message cut one
 * way here and another there would be translated differently depending on
 * whether the provider runs on this device or behind the server.
 */
const cases = JSON.parse(
  readFileSync(join(import.meta.dirname, "translation.cases.json"), "utf8"),
) as { name: string; source: string; segments: ({ keep: string } | { translate: string })[] }[];

describe("a message cut for translation", () => {
  for (const { name, source, segments } of cases) {
    it(name, () => {
      const expected = segments.map((segment) =>
        "keep" in segment
          ? { text: segment.keep, translate: false }
          : { text: segment.translate, translate: true },
      );
      const got = split(source);
      expect(got).toEqual(expected);
      expect(got.map((segment) => segment.text).join("")).toBe(source);
    });
  }

  it("puts back only what was sent", () => {
    const segments = split("run `make test` @amina");
    expect(translatable(segments)).toEqual(["run"]);
    expect(replace(segments, ["endesha"])).toBe("endesha `make test` @amina");
  });
});
