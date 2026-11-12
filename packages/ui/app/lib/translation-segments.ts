/*
 * What of a message is sent for translation, and what is kept.
 *
 * A message is the Markdown its author typed, and a translation service
 * rewrites everything it is given. Code, a mention, a link's address or an
 * email address rewritten is a message that now says something else — a
 * command that no longer runs, a mention of nobody — so those are cut out
 * before anything is sent and put back exactly as written.
 *
 * The same rules as server/internal/translation/segments.go, which translates
 * for the providers reached through the server; both are held to
 * translation.cases.json. Whitespace is space, tab and carriage return only,
 * and the patterns avoid \s and \w, because those mean different things in
 * JavaScript and Go — the two must cut the same message the same way.
 */

export interface Segment {
  text: string;
  translate: boolean;
}

const FENCE_OPEN = /^ {0,3}(`{3,}|~{3,})/;

const LINE_LEAD = /^[ \t]*(?:>[ \t]*)*(?:(?:[-*+]|[0-9]{1,9}[.)])[ \t]+)?(?:#{1,6}[ \t]+)?/;

const KEPT = new RegExp(
  [
    "``[^\\n]+?``",
    "`[^`\\n]+`",
    "\\]\\([^) \\t]*\\)",
    "https?://[^ \\t<>()]+",
    "www\\.[^ \\t<>()]+",
    "[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)+",
    "[@#][A-Za-z0-9_](?:[A-Za-z0-9_.:@-]*[A-Za-z0-9_])?",
    ":[a-z0-9_+-]+:",
  ].join("|"),
  "g",
);

const LETTER = /\p{L}/u;
const WORD_CHAR = /[A-Za-z0-9_]/;

function closesFence(line: string, fence: string): boolean {
  const indent = line.length - line.replace(/^ +/, "").length;
  const trimmed = line.replace(/^ +/, "").replace(/[ \t\r]+$/, "");
  if (indent > 3 || trimmed.length < fence.length) return false;
  return trimmed.split(fence[0]!).join("") === "";
}

/** Cuts a message body into segments. */
export function split(body: string): Segment[] {
  const segments: Segment[] = [];

  const add = (text: string, translate: boolean) => {
    if (!text) return;
    const last = segments[segments.length - 1];
    if (last && last.translate === translate) last.text += text;
    else segments.push({ text, translate });
  };
  const keep = (text: string) => add(text, false);

  // Words: whitespace at either edge is kept, so a service that trims what it
  // returns cannot run two pieces together, and a run with no letters is kept.
  const words = (chunk: string) => {
    const core = chunk.replace(/^[ \t\r]+/, "");
    keep(chunk.slice(0, chunk.length - core.length));
    const trimmed = core.replace(/[ \t\r]+$/, "");
    add(trimmed, LETTER.test(trimmed));
    keep(core.slice(trimmed.length));
  };

  let fence = "";
  body.split("\n").forEach((line, index) => {
    if (index > 0) keep("\n");

    if (fence) {
      keep(line);
      if (closesFence(line, fence)) fence = "";
      return;
    }
    const opened = FENCE_OPEN.exec(line);
    if (opened) {
      fence = opened[1]!;
      keep(line);
      return;
    }

    const lead = LINE_LEAD.exec(line)?.[0] ?? "";
    keep(lead);
    const rest = line.slice(lead.length);

    let position = 0;
    for (const found of rest.matchAll(KEPT)) {
      const start = found.index;
      const end = start + found[0].length;
      // "@" or "#" inside a word is not a mention: C#, F#, the end of an
      // address typed without a scheme.
      if (
        (rest[start] === "@" || rest[start] === "#") &&
        start > 0 &&
        WORD_CHAR.test(rest[start - 1]!)
      ) {
        continue;
      }
      words(rest.slice(position, start));
      keep(rest.slice(start, end));
      position = end;
    }
    words(rest.slice(position));
  });

  return segments;
}

/** The text of every segment to be sent, in order. */
export function translatable(segments: Segment[]): string[] {
  return segments.filter((segment) => segment.translate).map((segment) => segment.text);
}

/** Puts translations back in place of the segments that were sent. */
export function replace(segments: Segment[], translated: string[]): string {
  let next = 0;
  return segments
    .map((segment) =>
      segment.translate && next < translated.length ? translated[next++]! : segment.text,
    )
    .join("");
}
