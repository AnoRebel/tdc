import DOMPurify from "dompurify";
import { Marked, type Tokens } from "marked";

import { linkify } from "#layers/ui/app/lib/linkify";

/*
 * A message body, rendered.
 *
 * The body is the Markdown its author typed, and this turns a chat-sized
 * subset of it into markup: bold, italic, strikethrough, inline code, fenced
 * code blocks, quotes, lists and links. Everything else Markdown can say is
 * either text here or not said at all:
 *
 *   raw HTML       is text, escaped, never markup
 *   images         are a link to the address; attachments are how images
 *                  arrive, and a message must not fetch from wherever its
 *                  author pointed it
 *   headings       are a bold line, since a conversation has no use for a
 *                  title the size of a banner
 *   tables, rules, indented code, setext headings and link definitions
 *                  are text — each is something a person types by accident
 *                  far more often than on purpose ("----" under a line, four
 *                  spaces of indent)
 *
 * Links are found the way they always were: by linkify, over the text that is
 * left once the formatting is parsed, so an address, an email or a phone
 * number links exactly as it did before formatting existed, and nothing inside
 * code links at all. Markdown's own [text](address) links are kept, for http,
 * https and mailto addresses only.
 *
 * Three protections, each guarding something different. The parser is told
 * raw HTML is text. The links it emits are checked against a scheme list.
 * DOMPurify then cuts the result down to the dozen tags this can produce,
 * which is what catches a mistake in either of the other two.
 */

/** The tags rendering can produce, and so the only ones sanitising keeps. */
const ALLOWED_TAGS = [
  "p",
  "br",
  "strong",
  "em",
  "del",
  "code",
  "pre",
  "blockquote",
  "ul",
  "ol",
  "li",
  "a",
];
const ALLOWED_ATTR = ["href", "target", "rel", "class", "start"];

/** Where a link may point. Anything else is shown as its text. */
const SAFE_SCHEME = /^(https?:|mailto:)/i;

const ANCHOR_ATTRIBUTES = 'target="_blank" rel="noopener noreferrer nofollow"';

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function anchor(href: string, label: string): string {
  return `<a href="${escapeHtml(href)}" ${ANCHOR_ATTRIBUTES}>${label}</a>`;
}

/*
 * One parser, configured once. Private rather than marked's shared default so
 * that this configuration cannot leak into the attachment viewer's, or theirs
 * into this.
 */
const parser = new Marked({ gfm: true, breaks: false, async: false });

parser.use({
  /*
   * Returning undefined switches a rule off; returning false would hand the
   * text back to the rule's default, which is the opposite.
   */
  tokenizer: {
    html: () => undefined,
    tag: () => undefined,
    table: () => undefined,
    hr: () => undefined,
    code: () => undefined,
    lheading: () => undefined,
    def: () => undefined,
    url: () => undefined,
    autolink: () => undefined,
    // Two tildes only. A single one is how people write "about": "~5 minutes".
    del(source: string) {
      if (!source.startsWith("~~")) return undefined;
      return false;
    },
  },
  renderer: {
    heading({ tokens }: Tokens.Heading) {
      return `<p><strong>${this.parser.parseInline(tokens)}</strong></p>\n`;
    },
    link({ href, tokens }: Tokens.Link) {
      const label = this.parser.parseInline(tokens);
      return SAFE_SCHEME.test(href.trim()) ? anchor(href.trim(), label) : label;
    },
    image({ href, text }: Tokens.Image) {
      const label = escapeHtml(text || href);
      return SAFE_SCHEME.test(href.trim()) ? anchor(href.trim(), label) : label;
    },
    checkbox({ checked }: Tokens.Checkbox) {
      return checked ? "[x] " : "[ ] ";
    },
    code({ text, lang }: Tokens.Code) {
      const language = (lang ?? "").trim().split(/\s+/)[0]!.toLowerCase();
      const attribute = /^[a-z0-9+#.-]{1,20}$/.test(language)
        ? ` class="language-${escapeHtml(language)}"`
        : "";
      return `<pre><code${attribute}>${escapeHtml(text)}</code></pre>\n`;
    },
  },
});

/** Tags whose text is never searched for links. */
const NO_LINKS = new Set(["A", "CODE", "PRE"]);
/** Containers whose whitespace-only children are layout, not content. */
const BLOCKS = new Set(["BLOCKQUOTE", "UL", "OL", "LI", "TEMPLATE"]);

/**
 * Links the text that is left, and tidies the whitespace between blocks.
 *
 * A message bubble keeps its author's line breaks (white-space: pre-wrap), so
 * the newlines the parser puts between one block and the next would show as
 * blank lines. Those go; newlines inside a paragraph and a code block stay.
 */
function finish(root: DocumentFragment | Element): void {
  const walker = (node: Node) => {
    for (const child of [...node.childNodes]) {
      if (child.nodeType === 3) {
        const text = child.textContent ?? "";
        const parent = node as Element;
        if (!text.trim() && (node.nodeType === 11 || BLOCKS.has(parent.tagName))) {
          child.remove();
          continue;
        }
        const html = linkify(text);
        if (html !== escapeHtml(text)) {
          const holder = document.createElement("template");
          holder.innerHTML = html;
          child.replaceWith(holder.content);
        }
      } else if (child.nodeType === 1 && !NO_LINKS.has((child as Element).tagName)) {
        walker(child);
      }
    }
  };
  walker(root);
}

/** What the renderer needs to say in the reader's language. */
export interface RenderLabels {
  copy: string;
}

/**
 * Gives each code block a header: its language, and a button that copies it.
 *
 * Built as elements after sanitising rather than written into the markup
 * before it, so the only thing from the message that reaches the header is the
 * language name, set as text.
 */
function decorate(root: DocumentFragment, labels: RenderLabels): void {
  for (const pre of [...root.querySelectorAll("pre")]) {
    const code = pre.querySelector("code");
    const language = code?.className.match(/language-(\S+)/)?.[1] ?? "";

    const block = document.createElement("div");
    block.className = "code-block";
    const bar = document.createElement("div");
    bar.className = "code-block-bar";
    const name = document.createElement("span");
    name.textContent = language;
    const copy = document.createElement("button");
    copy.type = "button";
    copy.className = "code-copy";
    copy.setAttribute("data-copy-code", "");
    copy.textContent = labels.copy;

    bar.append(name, copy);
    pre.replaceWith(block);
    // Focusable, so a keyboard can scroll a long line into view.
    pre.tabIndex = 0;
    block.append(bar, pre);
  }
}

/*
 * Rendered bodies, kept. A conversation re-renders its list far more often
 * than its messages change, and parsing is the expensive part of drawing one.
 * Bounded, oldest first, so a long session does not hold every message it
 * ever scrolled past.
 */
const CACHE_LIMIT = 500;
const cache = new Map<string, string>();

/**
 * A message body as safe HTML.
 *
 * A body that is one paragraph comes back without the paragraph around it, so
 * it sits inline in its bubble exactly as plain text always has — beside the
 * "edited" marker, rather than pushing it onto a line of its own.
 */
export function renderMessage(body: string, labels: RenderLabels): string {
  const key = `${labels.copy}\u0000${body}`;
  const hit = cache.get(key);
  if (hit !== undefined) {
    cache.delete(key);
    cache.set(key, hit);
    return hit;
  }

  const parsed = parser.parse(body, { async: false }) as string;

  const holder = document.createElement("template");
  holder.innerHTML = parsed;
  finish(holder.content);

  const clean = DOMPurify.sanitize(holder.innerHTML, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    RETURN_DOM_FRAGMENT: true,
  });

  // A lone paragraph is unwrapped, as described above.
  const only = clean.children.length === 1 ? clean.firstElementChild : null;
  if (only?.tagName === "P" && clean.childNodes.length === 1) {
    only.replaceWith(...only.childNodes);
  }

  decorate(clean, labels);

  const holderOut = document.createElement("div");
  holderOut.append(clean);
  const html = holderOut.innerHTML;

  cache.set(key, html);
  if (cache.size > CACHE_LIMIT) cache.delete(cache.keys().next().value!);
  return html;
}

/*
 * The server's mention rule (server/internal/notify): "@name" at the start of a
 * word. Read the same way here, so what is drawn as a mention is what notified
 * somebody.
 */
const MENTION = /(^|\s)@([a-zA-Z0-9._-]{1,64})\b/g;

/**
 * Draws each "@username" in rendered markup as the person's name, the way it
 * reads in conversation, with the username it was written as kept on hover.
 *
 * Only where `nameFor` knows the name: a mention of nobody, or of somebody
 * this client has not been told about, stays exactly as typed. Never inside
 * code or a link. The name is set as text, so nothing in it becomes markup.
 */
export function nameMentions(html: string, nameFor: (username: string) => string | undefined) {
  if (!html.includes("@")) return html;

  const holder = document.createElement("template");
  holder.innerHTML = html;
  let changed = false;

  const walker = (node: Node) => {
    for (const child of [...node.childNodes]) {
      if (child.nodeType === 1) {
        if (!NO_LINKS.has((child as Element).tagName)) walker(child);
        continue;
      }
      if (child.nodeType !== 3) continue;
      const text = child.textContent ?? "";
      const pieces: Node[] = [];
      let last = 0;
      for (const match of text.matchAll(MENTION)) {
        const username = match[2]!;
        const name = nameFor(username);
        if (!name || name === username) continue;
        const at = match.index + match[1]!.length;
        pieces.push(document.createTextNode(text.slice(last, at)));
        const mention = document.createElement("span");
        mention.className = "mention";
        mention.title = `@${username}`;
        mention.textContent = name;
        pieces.push(mention);
        last = at + username.length + 1;
      }
      if (!pieces.length) continue;
      pieces.push(document.createTextNode(text.slice(last)));
      child.replaceWith(...pieces);
      changed = true;
    }
  };
  walker(holder.content);

  return changed ? holder.innerHTML : html;
}

/*
 * Syntax highlighting, loaded only when a code block names a language.
 *
 * The highlighter and its languages are one small module that nobody pays for
 * until a message with a labelled code block is on screen. Its tokens are
 * escaped and wrapped here rather than taken as markup from it.
 */

/** The names people write after a fence, mapped to what the highlighter calls them. */
const LANGUAGES: Record<string, string> = {
  js: "js",
  javascript: "js",
  jsx: "js",
  mjs: "js",
  ts: "ts",
  typescript: "ts",
  tsx: "ts",
  vue: "html",
  go: "go",
  golang: "go",
  py: "py",
  python: "py",
  rs: "rs",
  rust: "rs",
  sh: "bash",
  bash: "bash",
  shell: "bash",
  zsh: "bash",
  console: "bash",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  ini: "ini",
  sql: "sql",
  html: "html",
  xml: "xml",
  css: "css",
  c: "c",
  cpp: "c",
  "c++": "c",
  java: "java",
  kotlin: "java",
  lua: "lua",
  diff: "diff",
  patch: "diff",
  dockerfile: "docker",
  docker: "docker",
  makefile: "make",
  make: "make",
  md: "md",
  markdown: "md",
  http: "http",
  regex: "regex",
};

/** The highlighter language for a fence's label, or null when there is none. */
export function highlighterLanguage(label: string): string | null {
  return LANGUAGES[label.toLowerCase()] ?? null;
}

/**
 * Highlights the code blocks inside an element, once each.
 *
 * Runs after the body is on screen, so a slow load or a failed one costs the
 * colours and nothing else.
 */
export async function highlightWithin(root: HTMLElement): Promise<void> {
  const blocks = [...root.querySelectorAll<HTMLElement>("pre code[class*='language-']")].filter(
    (code) => !code.dataset.highlighted,
  );
  if (!blocks.length) return;

  const work = blocks
    .map((code) => ({
      code,
      language: highlighterLanguage(code.className.match(/language-(\S+)/)?.[1] ?? ""),
    }))
    .filter((block): block is { code: HTMLElement; language: string } => block.language !== null);
  if (!work.length) return;

  const { tokenize } = await import("@speed-highlight/core");

  for (const { code, language } of work) {
    const source = code.textContent ?? "";
    let html = "";
    try {
      await tokenize(source, language as never, (text, type) => {
        html += type
          ? `<span class="shj-syn-${type}">${escapeHtml(text)}</span>`
          : escapeHtml(text);
      });
    } catch {
      continue;
    }
    // The body may have been redrawn while the highlighter loaded.
    if (code.isConnected && code.textContent === source) {
      code.innerHTML = html;
      code.dataset.highlighted = "true";
    }
  }
}
