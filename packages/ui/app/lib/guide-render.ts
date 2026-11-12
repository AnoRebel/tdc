import DOMPurify from "dompurify";
import { Marked, type Token, type Tokens } from "marked";

import { createSlugger, resolveLink } from "#layers/ui/app/lib/guides";

/**
 * Turns a guide's markdown into what the help centre shows.
 *
 * Loaded only by the help centre's own pages, and only once one of them is
 * open: the parser and the sanitiser are not something anybody reading a
 * conversation should download.
 *
 * Two guards, as for an attachment, and for the same reason: the guides come
 * from this repository, but "this file is ours" is a claim about how the build
 * is run today, and a page in somebody's session should not depend on it. Raw
 * HTML in a guide is never emitted — comments are dropped and anything else is
 * shown as the text it is — and DOMPurify then sanitises what the parser did
 * produce.
 */

/** One entry in a guide's table of contents. */
export interface TocEntry {
  /** The anchor, as the guides link to it: integrating.md#api-tokens. */
  id: string;
  text: string;
  depth: 2 | 3;
}

export interface RenderedGuide {
  /** The guide's own first heading, shown as the page title. */
  title: string;
  html: string;
  toc: TocEntry[];
}

/** One searchable stretch of a guide: a heading and the prose beneath it. */
export interface GuideSection {
  /** Empty for the text before the first section heading. */
  id: string;
  heading: string;
  text: string;
}

/** What the rendered markup has to say in the reader's language. */
export interface RenderLabels {
  /** The button that copies a code block. */
  copy: string;
  /** The link beside a heading that points at it. */
  anchor: string;
}

/** A screenshot, where the build put it, and its size when that is known. */
export interface GuideImage {
  url: string;
  width?: number;
  height?: number;
}

/** Finds the screenshot an image in a guide names, or nothing. */
export type ImageLookup = (src: string) => GuideImage | undefined;

/**
 * The prefix on every heading's id in the page.
 *
 * Without it a heading called "Images" or "Forms" would produce an element
 * whose id shadows a property of the document, which is exactly what the
 * sanitiser strips — and the anchor would silently vanish. Links keep the bare
 * anchor; the page adds the prefix when it scrolls.
 */
export const ANCHOR_PREFIX = "guide-";

/** A heading token, once this file has given it an anchor. */
type Anchored = Tokens.Heading & { anchor?: string };

/**
 * A guide without the YAML front matter an editor may have put at its top.
 *
 * The guides carry none, but the public documentation site's editor (Studio)
 * writes a title and description there when it saves a page. Left in, the
 * parser would read the block as a rule followed by a heading made of YAML.
 */
export function withoutFrontMatter(source: string): string {
  return source.replace(/^﻿?---\r?\n[\s\S]*?\r?\n---[ \t]*(?:\r?\n|$)/, "");
}

export function renderGuide(
  source: string,
  labels: RenderLabels,
  images: ImageLookup = () => undefined,
): RenderedGuide {
  const parser = new Marked({ gfm: true, async: false });
  const tokens = parser.lexer(withoutFrontMatter(source));

  let title = "";
  const toc: TocEntry[] = [];
  const slug = createSlugger();

  parser.walkTokens(tokens, (token) => {
    if (token.type !== "heading") return;
    const heading = token as Anchored;
    const text = plainText(heading.tokens);
    heading.anchor = slug(text);

    if (heading.depth === 1 && !title) title = text;
    if (heading.depth === 2 || heading.depth === 3) {
      toc.push({ id: heading.anchor, text, depth: heading.depth });
    }
  });

  parser.use({
    renderer: {
      heading(token) {
        const { anchor = "", depth, tokens: inline } = token as Anchored;
        // The title is the page's own heading, drawn by the page.
        if (depth === 1) return "";
        const id = escapeAttribute(anchor);
        return (
          `<h${depth} id="${ANCHOR_PREFIX}${id}">${this.parser.parseInline(inline)}` +
          ` <a class="guide-anchor" href="#${id}" data-section="${id}"` +
          ` aria-label="${escapeAttribute(labels.anchor)}">#</a></h${depth}>\n`
        );
      },

      link({ href, title: linkTitle, tokens: inline }) {
        const text = this.parser.parseInline(inline);
        const titled = linkTitle ? ` title="${escapeAttribute(linkTitle)}"` : "";
        const resolved = resolveLink(href);

        switch (resolved.kind) {
          case "guide": {
            const hash = resolved.hash ? `#${resolved.hash}` : "";
            return (
              `<a href="/help/${resolved.slug}${escapeAttribute(hash)}"${titled}` +
              ` data-guide="${resolved.slug}" data-section="${escapeAttribute(resolved.hash)}">${text}</a>`
            );
          }
          case "section":
            return (
              `<a href="#${escapeAttribute(resolved.hash)}"${titled}` +
              ` data-section="${escapeAttribute(resolved.hash)}">${text}</a>`
            );
          case "external":
            return (
              `<a href="${escapeAttribute(resolved.href)}"${titled}` +
              ` target="_blank" rel="noopener noreferrer">${text}</a>`
            );
          case "none":
            return text;
        }
      },

      /*
       * A screenshot from docs/images, at the address the build gave it.
       * Anything else — an image from elsewhere, or one that is missing — is
       * not something a help page needs to load, and its description stands
       * in for it. Lazy, and sized where the size is known, so a long guide
       * fetches only what is scrolled to and nothing jumps when it arrives.
       */
      image({ href, text }) {
        const found = images(href);
        if (!found) return escapeHtml(text);
        const size =
          found.width && found.height ? ` width="${found.width}" height="${found.height}"` : "";
        return (
          `<img src="${escapeAttribute(found.url)}" alt="${escapeAttribute(text)}"${size}` +
          ` loading="lazy" decoding="async">`
        );
      },

      html({ text }) {
        if (/^\s*<!--[\s\S]*-->\s*$/.test(text)) return "";
        return escapeHtml(text);
      },

      code({ text, lang }) {
        const language = (lang ?? "").split(/\s/)[0] ?? "";
        return (
          `<div class="guide-code">` +
          `<div class="guide-code-bar"><span>${escapeHtml(language)}</span>` +
          `<button type="button" class="guide-copy" data-copy="">${escapeHtml(labels.copy)}</button></div>` +
          `<pre><code>${escapeHtml(text)}</code></pre></div>\n`
        );
      },
    },
  });

  const html = (parser.parser(tokens) as string)
    // Wide tables scroll on their own rather than widening the page.
    .replace(/<table>/g, '<div class="guide-table"><table>')
    .replace(/<\/table>/g, "</table></div>");

  return { title, html: sanitise(html), toc };
}

/**
 * The sections of a guide, as plain text, for searching.
 *
 * Code is left out: a search for "token" should land on the paragraph that
 * explains tokens rather than on every request example that carries one.
 */
export function guideSections(source: string): { title: string; sections: GuideSection[] } {
  const tokens = new Marked({ gfm: true }).lexer(withoutFrontMatter(source));
  const slug = createSlugger();

  let title = "";
  const sections: GuideSection[] = [];
  let current: GuideSection = { id: "", heading: "", text: "" };

  for (const token of tokens) {
    if (token.type === "heading") {
      const heading = token as Tokens.Heading;
      const text = plainText(heading.tokens);
      const anchor = slug(text);
      if (heading.depth === 1) {
        title ||= text;
        continue;
      }
      if (current.text || current.heading) sections.push(current);
      current = { id: anchor, heading: text, text: "" };
      continue;
    }
    if (token.type === "code" || token.type === "html" || token.type === "space") continue;
    const text = plainText([token]).replace(/\s+/g, " ").trim();
    if (text) current.text += (current.text ? " " : "") + text;
  }
  if (current.text || current.heading) sections.push(current);

  return { title, sections };
}

/** What a run of tokens says, without the markup. */
export function plainText(tokens: readonly Token[] | undefined): string {
  if (!tokens) return "";
  let text = "";

  for (const token of tokens) {
    switch (token.type) {
      case "html":
      case "code":
      case "space":
      case "hr":
        break;
      case "br":
        text += " ";
        break;
      case "list":
        text += (token as Tokens.List).items.map((item) => plainText(item.tokens)).join(" ");
        break;
      case "table": {
        const table = token as Tokens.Table;
        const cells = [table.header, ...table.rows].flat();
        text += cells.map((cell) => plainText(cell.tokens)).join(" ");
        break;
      }
      default:
        if ("tokens" in token && token.tokens) text += plainText(token.tokens);
        else if ("text" in token && typeof token.text === "string")
          text += decodeEntities(token.text);
        if (token.type === "paragraph" || token.type === "heading") text += " ";
    }
  }

  return text.replace(/\s+/g, " ").trim();
}

function sanitise(html: string): string {
  return DOMPurify.sanitize(html, {
    // Set deliberately alongside rel="noopener", which is the mitigation for
    // the tab-nabbing DOMPurify strips target to prevent.
    ADD_ATTR: ["target", "loading", "decoding"],
    // img is allowed only because the image renderer above is the one thing
    // that can produce it: a guide's own <img> is escaped as text before here.
    FORBID_TAGS: ["style", "script", "iframe", "object", "embed", "form", "input", "svg", "math"],
    FORBID_ATTR: ["style"],
  });
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function escapeAttribute(value: string): string {
  return escapeHtml(value);
}

/** The few entities the lexer leaves in text, back to the characters they are. */
function decodeEntities(value: string): string {
  return value
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&amp;/g, "&");
}
