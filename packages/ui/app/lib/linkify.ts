/**
 * Turns the parts of a message people expect to be clickable into links.
 *
 * A URL, an email address or a phone number sitting inert in a conversation is
 * a small daily annoyance: everybody has tried to tap one. This finds them and
 * marks them up.
 *
 * The safety property is structural rather than a matter of getting a
 * sanitiser's configuration right. Every character of the message is escaped
 * first, and anchors are inserted only around spans this file matched itself.
 * Message text therefore never becomes markup — there is no path by which it
 * could, because nothing from the message is ever emitted unescaped. Callers
 * still sanitise the result; that is a second line of defence rather than the
 * only one.
 */

/** What the message body is scanned for, in the order the scanner tries them. */
type Kind = "url" | "email" | "phone";

/*
 * One pattern per kind, combined into a single scan.
 *
 * Alternation order matters: an email address contains no scheme but a URL may
 * contain an @ (https://user@host), so URLs are tried first. Phone numbers are
 * tried last because their pattern is the loosest and would otherwise claim
 * digits inside a URL's path or port.
 */
const URL_PATTERN = String.raw`https?:\/\/[^\s<>"']+`;

// A bare host with a known-looking TLD, so "tdc.or.tz" links without anybody
// having to type a scheme. Deliberately requires a dot and at least two
// letters at the end, which keeps "e.g" and "v1.2" out of it.
const BARE_HOST_PATTERN = String.raw`(?:www\.[^\s<>"']+|[a-z0-9][-a-z0-9]*(?:\.[a-z0-9][-a-z0-9]*)*\.[a-z]{2,}(?:\/[^\s<>"']*)?)`;

const EMAIL_PATTERN = String.raw`[^\s<>"'@]+@[a-z0-9][-a-z0-9]*(?:\.[a-z0-9][-a-z0-9]*)*\.[a-z]{2,}`;

/*
 * Phone numbers, deliberately conservative.
 *
 * Tanzanian numbers are written +255 712 345 678, 0712 345 678 and
 * 0712-345-678, all of which should link. A loose pattern would also claim
 * order numbers, version strings and years, so this requires either a leading
 * + or at least nine digits, and refuses a run of digits that is simply long.
 */
const PHONE_PATTERN = String.raw`\+?\d[\d\s().-]{7,}\d`;

const SCANNER = new RegExp(
  `(${URL_PATTERN})|(${EMAIL_PATTERN})|(${BARE_HOST_PATTERN})|(${PHONE_PATTERN})`,
  "gi",
);

/**
 * Trailing punctuation belongs to the sentence, not to what was matched.
 *
 * "see https://tdc.or.tz." should link the address without the full stop, and
 * "(https://tdc.or.tz)" without the bracket. A closing bracket is only trimmed
 * when the match does not open one itself, so a Wikipedia URL ending in ")"
 * survives.
 */
function trimTrailing(text: string): string {
  let trimmed = text.replace(/[.,;:!?'"]+$/, "");

  while (/[)\]}]$/.test(trimmed)) {
    const closing = trimmed.at(-1)!;
    const opening = { ")": "(", "]": "[", "}": "{" }[closing]!;

    const opens = trimmed.split(opening).length - 1;
    const closes = trimmed.split(closing).length - 1;
    if (opens >= closes) break;

    trimmed = trimmed.slice(0, -1);
  }

  return trimmed;
}

/** A phone number is only plausible if it has enough digits to be one. */
function isPlausiblePhone(text: string): boolean {
  const digits = text.replace(/\D/g, "");

  // Long enough to be a number, short enough not to be an identifier that
  // happens to be digits. E.164 allows fifteen.
  if (digits.length < 9 || digits.length > 15) return false;

  // A run of digits with no separators and no country code is far more likely
  // to be an order number or an identifier than a phone number.
  return text.startsWith("+") || /[\s().-]/.test(text);
}

function escape(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/** Where a match should point. */
function hrefFor(kind: Kind, text: string): string {
  switch (kind) {
    case "email":
      return `mailto:${text}`;
    case "phone":
      // Spaces and punctuation are for reading; tel: wants the digits.
      return `tel:${text.replace(/[\s().-]/g, "")}`;
    case "url":
      // A bare host needs a scheme or the browser resolves it against the
      // current page. https rather than http: a link that fails to upgrade is
      // better than one that quietly does not encrypt.
      return /^https?:\/\//i.test(text) ? text : `https://${text}`;
  }
}

/**
 * Renders a message body as HTML with its links, addresses and numbers marked.
 *
 * The result still needs sanitising by the caller before it reaches the page.
 */
export function linkify(body: string): string {
  let html = "";
  let cursor = 0;

  for (const match of body.matchAll(SCANNER)) {
    const raw = match[0];
    const start = match.index;

    const text = trimTrailing(raw);
    if (!text) continue;

    // Which alternative matched. Groups are ordered as in SCANNER.
    const kind: Kind = match[1] ? "url" : match[2] ? "email" : match[3] ? "url" : "phone";

    if (kind === "phone" && !isPlausiblePhone(text)) continue;

    html += escape(body.slice(cursor, start));
    html +=
      `<a href="${escape(hrefFor(kind, text))}"` +
      ` target="_blank" rel="noopener noreferrer nofollow">${escape(text)}</a>`;

    cursor = start + text.length;
  }

  html += escape(body.slice(cursor));
  return html;
}
