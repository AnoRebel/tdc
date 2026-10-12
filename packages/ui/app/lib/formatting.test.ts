import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { applyFormat, insideOpenFence, plainText, withoutCode, type Draft } from "./formatting";

/*
 * The same cases the server and the desktop application are tested against,
 * so a notification, a push banner and the pinned list cannot disagree about
 * what a message says once its formatting is gone.
 */
const cases = JSON.parse(
  readFileSync(join(import.meta.dirname, "plaintext.cases.json"), "utf8"),
) as { name: string; source: string; plain: string }[];

describe("a message as plain text", () => {
  for (const { name, source, plain } of cases) {
    it(name, () => expect(plainText(source)).toBe(plain));
  }
});

describe("a message without its code", () => {
  it("drops code spans and blocks, and keeps the rest", () => {
    expect(
      withoutCode("see https://a.example and `https://b.example`\n```\nhttps://c.example\n```"),
    ).toBe("see https://a.example and ");
  });
});

/** A draft written with brackets marking the selection: "a [b] c". */
function draft(marked: string): Draft {
  const start = marked.indexOf("[");
  const end = marked.indexOf("]") - 1;
  return { text: marked.replace("[", "").replace("]", ""), start, end };
}

function show({ text, start, end }: Draft): string {
  return `${text.slice(0, start)}[${text.slice(start, end)}]${text.slice(end)}`;
}

describe("wrapping the selection in the composer", () => {
  it("wraps a selection, and keeps it selected", () => {
    expect(show(applyFormat(draft("say [hello] now"), "bold"))).toBe("say **[hello]** now");
    expect(show(applyFormat(draft("say [hello] now"), "italic"))).toBe("say _[hello]_ now");
    expect(show(applyFormat(draft("[gone]"), "strikethrough"))).toBe("~~[gone]~~");
    expect(show(applyFormat(draft("run [ls]"), "code"))).toBe("run `[ls]`");
  });

  it("puts an empty pair in with the cursor between", () => {
    expect(show(applyFormat(draft("a []"), "bold"))).toBe("a **[]**");
  });

  it("leaves spaces the selection picked up outside the markers", () => {
    expect(show(applyFormat(draft("[word ]next"), "bold"))).toBe("**[word]** next");
  });

  it("unwraps what is already wrapped, from inside or around", () => {
    expect(show(applyFormat(draft("say **[hello]** now"), "bold"))).toBe("say [hello] now");
    expect(show(applyFormat(draft("say [**hello**] now"), "bold"))).toBe("say [hello] now");
  });

  it("puts a code block on lines of its own", () => {
    expect(show(applyFormat(draft("see [x := 1] ok"), "codeBlock"))).toBe(
      "see \n```\n[x := 1]\n```\n ok",
    );
    expect(show(applyFormat(draft("[]"), "codeBlock"))).toBe("```\n[]\n```");
  });
});

describe("knowing when Enter is a new line rather than send", () => {
  it("is a new line inside an open fence, and send once it closes", () => {
    expect(insideOpenFence("```js\nconst a = 1", 17)).toBe(true);
    expect(insideOpenFence("```js", 5)).toBe(true);
    expect(insideOpenFence("```js\nconst a = 1\n```", 21)).toBe(false);
    expect(insideOpenFence("plain words", 11)).toBe(false);
    expect(insideOpenFence("an inline ```fence``` is not one", 32)).toBe(false);
  });

  it("only counts what is before the cursor", () => {
    const text = "before\n```\ncode\n```";
    expect(insideOpenFence(text, 6)).toBe(false);
    expect(insideOpenFence(text, 15)).toBe(true);
  });
});
