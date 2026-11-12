/*
 * Message formatting, for everything that is not the renderer.
 *
 * A message body is stored as the Markdown its author typed. Two things here
 * work on that source without parsing it as a whole document: turning it into
 * plain text for the places that cannot show formatting, and the composer's
 * wrapping of a selection in markers.
 *
 * Deliberately free of the Markdown parser. This file is imported by places a
 * first visit already pays for, and the parser is the size of a small feature;
 * the renderer that needs it loads with the conversation.
 *
 * The plain-text rules mirror the server's (server/internal/markup), which
 * strips the same way before a body reaches a notification, a push payload or
 * the search index. The two are tested against the same cases.
 */

/** Escapable ASCII punctuation, as CommonMark defines it. */
const ESCAPABLE = /\\([!-/:-@[-`{-~])/g;

/*
 * An escaped character is parked in the private-use area while the emphasis
 * rules run, so "\*not italic\*" cannot be taken for markers, and put back
 * afterwards as the character it stood for.
 */
const PARK = 0xe000;
const park = (text: string) =>
  text.replace(ESCAPABLE, (_, character: string) =>
    String.fromCharCode(PARK + character.charCodeAt(0)),
  );
const unpark = (text: string) =>
  text.replace(/[-]/g, (character) => String.fromCharCode(character.charCodeAt(0) - PARK));

/** A fence opening or closing a code block: three or more backticks or tildes. */
const FENCE = /^ {0,3}(`{3,}|~{3,})(.*)$/;

/**
 * Splits a line into code spans and the text between them.
 *
 * A scan rather than a pattern, because a span closes on a run of exactly as
 * many backticks as opened it — which needs a backreference, and the server's
 * regular expressions have none. The same scan on both sides keeps them equal.
 */
function splitCode(line: string): { code: boolean; text: string }[] {
  const parts: { code: boolean; text: string }[] = [];
  let cursor = 0;
  let index = 0;

  while (index < line.length) {
    if (line[index] !== "`") {
      index++;
      continue;
    }

    let run = 1;
    while (line[index + run] === "`") run++;

    // The closing run: the next run of exactly the same length.
    let search = index + run;
    let close = -1;
    while (search < line.length) {
      if (line[search] !== "`") {
        search++;
        continue;
      }
      let length = 1;
      while (line[search + length] === "`") length++;
      if (length === run) {
        close = search;
        break;
      }
      search += length;
    }

    if (close === -1) {
      index += run;
      continue;
    }

    if (index > cursor) parts.push({ code: false, text: line.slice(cursor, index) });
    let inner = line.slice(index + run, close);
    // One space of padding either side is how a span holds a backtick.
    if (/^ .*[^ ].* $/.test(inner)) inner = inner.slice(1, -1);
    parts.push({ code: true, text: inner });
    cursor = index = close + run;
  }

  if (cursor < line.length) parts.push({ code: false, text: line.slice(cursor) });
  return parts;
}

/** Emphasis, links and escapes out of a run of text that is not code. */
function plainInline(text: string): string {
  return unpark(
    park(text)
      // A link keeps its address: in plain text the address is the link.
      .replace(/!?\[([^\]]*)\]\(\s*<?([^\s)>]*)>?(?:\s+"[^"]*")?\s*\)/g, (_, label, url) =>
        !label || label === url ? url : `${label} (${url})`,
      )
      .replace(/\*\*(\S(?:.*?\S)??)\*\*/g, "$1")
      .replace(/__(\S(?:.*?\S)??)__/g, "$1")
      .replace(/~~(\S(?:.*?\S)??)~~/g, "$1")
      .replace(/\*(\S(?:.*?\S)??)\*/g, "$1")
      // Underscores only at word edges, so snake_case_names survive.
      .replace(/(^|[^A-Za-z0-9_])_(\S(?:.*?\S)??)_(?![A-Za-z0-9_])/g, "$1$2"),
  );
}

/**
 * A message body as plain text.
 *
 * For whatever cannot show formatting: a notification, a confirmation that
 * quotes the message, a pinned list, a title. The words survive and the
 * markers do not — code keeps its contents, a link keeps its address, a quote
 * and a heading lose only their marker. Lists keep theirs, which read as
 * plainly as they write.
 */
export function plainText(source: string): string {
  const out: string[] = [];
  let fence: string | null = null;

  for (const line of source.split("\n")) {
    if (fence) {
      const closing = line.match(FENCE);
      if (closing && closing[1]![0] === fence[0] && closing[1]!.length >= fence.length) {
        if (!closing[2]!.trim()) {
          fence = null;
          continue;
        }
      }
      out.push(line);
      continue;
    }

    const opening = line.match(FENCE);
    if (opening && !(opening[1]![0] === "`" && opening[2]!.includes("`"))) {
      fence = opening[1]!;
      continue;
    }

    const stripped = line
      .replace(/^ {0,3}(?:> ?)+/, "")
      .replace(/^ {0,3}#{1,6}[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$/, "$1")
      .replace(/^(\s*)[*+][ \t]+/, "$1- ");

    out.push(
      splitCode(stripped)
        .map((part) => (part.code ? part.text : plainInline(part.text)))
        .join(""),
    );
  }

  return out.join("\n");
}

/**
 * A body with its code removed, for anything that looks for things people
 * meant rather than things they quoted — a link to preview, for one. A URL in
 * a code block is an example, not a recommendation.
 */
export function withoutCode(source: string): string {
  const out: string[] = [];
  let fence: string | null = null;

  for (const line of source.split("\n")) {
    const marker = line.match(FENCE);
    if (fence) {
      if (
        marker &&
        marker[1]![0] === fence[0] &&
        marker[1]!.length >= fence.length &&
        !marker[2]!.trim()
      ) {
        fence = null;
      }
      continue;
    }
    if (marker && !(marker[1]![0] === "`" && marker[2]!.includes("`"))) {
      fence = marker[1]!;
      continue;
    }
    out.push(
      splitCode(line)
        .filter((part) => !part.code)
        .map((part) => part.text)
        .join(" "),
    );
  }

  return out.join("\n");
}

/*
 * The composer's side: wrapping what is selected.
 */

/** What the composer holds: its text, and where the selection runs. */
export interface Draft {
  text: string;
  start: number;
  end: number;
}

/** The formatting a shortcut or toolbar button applies. */
export const FORMATS = ["bold", "italic", "strikethrough", "code", "codeBlock"] as const;
export type Format = (typeof FORMATS)[number];

const MARKERS: Record<Exclude<Format, "codeBlock">, string> = {
  bold: "**",
  italic: "_",
  strikethrough: "~~",
  code: "`",
};

/**
 * Applies a format to the selection, and says where the selection goes after.
 *
 * Pressing it again on text already wrapped unwraps it, which is what every
 * editor these people use does, and what makes a mistaken press undoable
 * without reaching for the mouse. With nothing selected the markers go in as a
 * pair with the cursor between them, ready to type into.
 *
 * A code block goes on lines of its own, since a fence is only a fence at the
 * start of a line.
 */
export function applyFormat(draft: Draft, format: Format): Draft {
  const { text, start, end } = draft;
  const selected = text.slice(start, end);

  if (format === "codeBlock") {
    const before = text.slice(0, start);
    const after = text.slice(end);
    const lead = before === "" || before.endsWith("\n") ? "" : "\n";
    const trail = after === "" || after.startsWith("\n") ? "" : "\n";
    const opening = `${lead}\`\`\`\n`;
    const body = selected;
    const closing = `${body.endsWith("\n") || body === "" ? "" : "\n"}\`\`\`${trail}`;

    const result = before + opening + body + (body === "" ? "\n" : "") + closing + after;
    const caret = before.length + opening.length;
    return { text: result, start: caret, end: caret + body.length };
  }

  const marker = MARKERS[format];
  const length = marker.length;

  // Already wrapped, inside the selection or around it: take the markers off.
  if (selected.length >= length * 2 && selected.startsWith(marker) && selected.endsWith(marker)) {
    const inner = selected.slice(length, -length);
    return {
      text: text.slice(0, start) + inner + text.slice(end),
      start,
      end: start + inner.length,
    };
  }
  if (text.slice(start - length, start) === marker && text.slice(end, end + length) === marker) {
    return {
      text: text.slice(0, start - length) + selected + text.slice(end + length),
      start: start - length,
      end: end - length,
    };
  }

  /*
   * Spaces the selection picked up at its edges stay outside the markers.
   * Double-clicking a word selects its trailing space on several platforms,
   * and "**word **" is not bold — the closing marker must touch a character.
   */
  const leading = selected.length - selected.trimStart().length;
  const trailing = selected.length - selected.trimEnd().length;
  const core = selected.trim();

  if (!core) {
    const result = text.slice(0, start) + marker + marker + text.slice(end);
    return { text: result, start: start + length, end: start + length };
  }

  const wrapped =
    selected.slice(0, leading) +
    marker +
    core +
    marker +
    selected.slice(selected.length - trailing);
  return {
    text: text.slice(0, start) + wrapped + text.slice(end),
    start: start + leading + length,
    end: start + leading + length + core.length,
  };
}

/**
 * Whether the cursor is inside a code block that has been opened and not yet
 * closed.
 *
 * Enter sends a message, except here: somebody pasting or typing code wants
 * lines, and sending half a code block is never what they meant. Once the
 * closing fence is typed, Enter sends again.
 */
export function insideOpenFence(text: string, cursor: number): boolean {
  let fence: string | null = null;

  for (const line of text.slice(0, cursor).split("\n")) {
    const marker = line.match(FENCE);
    if (!marker) continue;
    if (fence) {
      if (marker[1]![0] === fence[0] && marker[1]!.length >= fence.length && !marker[2]!.trim()) {
        fence = null;
      }
    } else if (!(marker[1]![0] === "`" && marker[2]!.includes("`"))) {
      fence = marker[1]!;
    }
  }

  return fence !== null;
}
