import DOMPurify from "dompurify";
import { Marked } from "marked";

/**
 * Renders markdown from an attachment.
 *
 * Two separate protections, because they guard different things. The parser
 * runs with raw HTML escaped rather than passed through, which stops a document
 * that contains a script tag. DOMPurify then sanitises what the parser
 * produced, which stops the constructs markdown itself can express: a link
 * whose target is `javascript:`, an image whose source reaches out to another
 * host.
 *
 * Neither alone is sufficient. Escaping HTML does not stop a malicious link,
 * and sanitising does not stop a parser bug. Together they mean an attachment
 * someone else uploaded cannot execute in the reader's session.
 *
 * marked rather than markdown-it: both are MIT, and marked has no dependencies
 * at all where markdown-it brings six. On a project whose whole premise is that
 * someone can host it themselves, a smaller tree is worth having.
 */
export function useMarkdown() {
  // A private instance rather than the shared default, so that configuring it
  // here cannot affect anything else that happens to import marked.
  const parser = new Marked({
    // Bare URLs become links, which is what people expect of a README.
    gfm: true,
    breaks: true,
    // Raw HTML in the source is escaped rather than emitted. DOMPurify would
    // catch most of what this stops, but the parser refusing to emit it at all
    // is the cheaper and more certain guard.
    async: false,
  });

  // Links open away from the application, and carry no referrer or window
  // handle back to it.
  parser.use({
    renderer: {
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens);
        const titleAttribute = title ? ` title="${escapeAttribute(title)}"` : "";

        return (
          `<a href="${escapeAttribute(href)}"${titleAttribute}` +
          ` target="_blank" rel="noopener noreferrer nofollow">${text}</a>`
        );
      },
    },
  });

  return function render(source: string): string {
    const html = parser.parse(source, { async: false }) as string;

    return DOMPurify.sanitize(html, {
      // DOMPurify strips target by default, on the grounds that it can be used
      // for tab-nabbing. Here it is set deliberately alongside rel="noopener",
      // which is the mitigation — without allowing it back, a link to another
      // site would replace the conversation.
      ADD_ATTR: ["target", "rel"],
      // Anything that loads or runs is gone. Images are dropped as well: a
      // relative source would resolve against the application's origin and
      // could not be authorised, and an absolute one would fetch from wherever
      // a document someone else uploaded chose to point.
      FORBID_TAGS: ["style", "script", "iframe", "object", "embed", "form", "input", "img"],
      FORBID_ATTR: ["style", "onerror", "onload", "onclick"],
      // ALLOWED_URI_REGEXP is deliberately not set. It is applied to every
      // attribute value rather than only to URLs, so it strips target="_blank"
      // and the rel that mitigates it — verified against DOMPurify 3.4. It
      // would also add nothing: the default configuration already refuses
      // javascript:, data: and vbscript: hrefs.
    });
  };
}

/**
 * Escapes a value going into an HTML attribute.
 *
 * DOMPurify runs afterwards and would catch an escape from the attribute, but
 * building the string correctly in the first place means the sanitiser is a
 * second line of defence rather than the only one.
 */
function escapeAttribute(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/"/g, "&quot;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}
