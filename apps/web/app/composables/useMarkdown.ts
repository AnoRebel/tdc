import DOMPurify from "dompurify";
import MarkdownIt from "markdown-it";

/**
 * Renders markdown from an attachment.
 *
 * Two separate protections, because they guard different things. markdown-it
 * runs with HTML disabled, so raw markup inside the document is escaped rather
 * than parsed — that stops a document that contains a script tag. DOMPurify
 * then sanitises what markdown-it produced, which stops the constructs markdown
 * itself can express: a link whose target is `javascript:`, an image whose
 * handler is an attribute.
 *
 * Neither alone is sufficient. Disabling HTML does not stop a malicious link,
 * and sanitising does not stop a parser bug. Together they mean an attachment
 * someone else uploaded cannot execute in the reader's session.
 */
export function useMarkdown() {
  const renderer = new MarkdownIt({
    // Raw HTML in the source is escaped, not parsed.
    html: false,
    // Bare URLs become links, which is what people expect of a README.
    linkify: true,
    // Quotes and dashes; cosmetic, and it makes prose read properly.
    typographer: true,
    breaks: true,
  });

  // Links open away from the application, and carry no referrer or window
  // handle back to it.
  const defaultLinkOpen =
    renderer.renderer.rules.link_open ??
    ((tokens, index, options, _env, self) => self.renderToken(tokens, index, options));

  renderer.renderer.rules.link_open = (tokens, index, options, env, self) => {
    tokens[index]?.attrSet("target", "_blank");
    tokens[index]?.attrSet("rel", "noopener noreferrer nofollow");
    return defaultLinkOpen(tokens, index, options, env, self);
  };

  return function render(source: string): string {
    return DOMPurify.sanitize(renderer.render(source), {
      // DOMPurify strips target by default, on the grounds that it can be used
      // for tab-nabbing. Here it is set deliberately alongside rel="noopener",
      // which is the mitigation — without allowing it back, a link to another
      // site would replace the conversation.
      ADD_ATTR: ["target", "rel"],
      // Anything that loads or runs is gone: an attachment must not be able to
      // reach the network on the reader's behalf or execute in their session.
      FORBID_TAGS: ["style", "script", "iframe", "object", "embed", "form", "input", "img"],
      FORBID_ATTR: ["style", "onerror", "onload", "onclick"],
      // ALLOWED_URI_REGEXP is deliberately not set. It is applied to every
      // attribute value rather than only to URLs, so it strips target="_blank"
      // and the rel that mitigates it — verified against DOMPurify 3.4. It
      // would also add nothing: the default configuration already refuses
      // javascript:, data: and vbscript: hrefs.
      //
      // Images are dropped for a different reason, below: a relative source
      // would resolve against the application's origin and could not be
      // authorised, and an absolute one would fetch from wherever a document
      // someone else uploaded chose to point.
    });
  };
}
