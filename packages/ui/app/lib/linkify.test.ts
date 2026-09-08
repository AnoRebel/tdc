import { describe, expect, it } from "vitest";

import { linkify } from "./linkify";

/*
 * The safety property first, since it is the reason this file builds markup by
 * hand rather than handing the body to a library.
 */
describe("escaping", () => {
  it("never lets message text become markup", () => {
    /*
     * Each attempt carries a link as well as the markup.
     *
     * Text is escaped in three places — before a match, inside one, and after
     * the last — and a payload with no link at all only ever reaches the
     * third. Verified by removing each escape in turn: without a link in the
     * string, the first two can be deleted and these tests still pass.
     */
    const attempts = [
      "<script>alert(1)</script> https://tdc.or.tz",
      "https://tdc.or.tz <img src=x onerror=alert(1)>",
      'https://tdc.or.tz " onmouseover="alert(1)',
      "<a href='javascript:alert(1)'>click</a> https://tdc.or.tz more",
      "before </a><script>alert(1)</script> https://tdc.or.tz after",
      "<b>https://tdc.or.tz</b>",
    ];

    for (const attempt of attempts) {
      const html = linkify(attempt);

      /*
       * The check is that no tag survives, not that the words do. Escaped
       * text may well contain "onerror" — as the five characters of a
       * sentence, which is inert. Asserting on the substring would fail on
       * correct output and pass on nothing useful.
       */
      expect(html).not.toContain("<script");
      expect(html).not.toContain("<img");
      // The only tag this function ever emits is its own anchor.
      expect(html.replace(/<a href="[^"]*" target="_blank" rel="[^"]*">|<\/a>/g, "")).not.toMatch(
        /<[a-z/]/i,
      );
    }
  });

  it("escapes a message that contains no links at all", () => {
    expect(linkify("5 < 6 && 7 > 2")).toBe("5 &lt; 6 &amp;&amp; 7 &gt; 2");
  });

  /*
   * The matched span is attacker-controlled too, and it is emitted twice: once
   * into the href and once as the link's text. Both need escaping.
   *
   * An ampersand is the character that reaches them in practice — the URL
   * pattern excludes quotes and angle brackets, so a query string is what
   * actually exercises these two escapes. Verified by removing each in turn.
   */
  it("escapes the address in both the href and the link text", () => {
    const html = linkify("https://tdc.or.tz/?a=1&b=2");

    expect(html).toContain('href="https://tdc.or.tz/?a=1&amp;b=2"');
    expect(html).toContain(">https://tdc.or.tz/?a=1&amp;b=2</a>");
    // The raw ampersand appears nowhere: every one of them is escaped.
    expect(html).not.toMatch(/&(?!amp;|lt;|gt;|quot;|#39;)/);
  });

  it("escapes an ampersand in an email address", () => {
    const html = linkify("habari&x@tdc.or.tz");
    expect(html).toContain('href="mailto:habari&amp;x@tdc.or.tz"');
    expect(html).not.toMatch(/&(?!amp;|lt;|gt;|quot;|#39;)/);
  });

  it("refuses a javascript: scheme by never matching one", () => {
    const html = linkify("javascript:alert(1)");
    expect(html).not.toContain('href="javascript:');
  });
});

describe("web addresses", () => {
  it("links a plain URL", () => {
    expect(linkify("see https://tdc.or.tz today")).toBe(
      'see <a href="https://tdc.or.tz" target="_blank" rel="noopener noreferrer nofollow">' +
        "https://tdc.or.tz</a> today",
    );
  });

  it("links a bare host without a scheme, and supplies https", () => {
    expect(linkify("tdc.or.tz")).toContain('href="https://tdc.or.tz"');
  });

  it("leaves a sentence's full stop out of the link", () => {
    const html = linkify("Read https://tdc.or.tz.");
    expect(html).toContain(">https://tdc.or.tz</a>.");
  });

  it("keeps a closing bracket the URL opened itself", () => {
    const html = linkify("https://en.wikipedia.org/wiki/Dodoma_(city)");
    expect(html).toContain(">https://en.wikipedia.org/wiki/Dodoma_(city)</a>");
  });

  it("drops a closing bracket the sentence opened", () => {
    const html = linkify("(https://tdc.or.tz)");
    expect(html).toContain(">https://tdc.or.tz</a>)");
  });

  it("opens links away from the conversation", () => {
    const html = linkify("https://tdc.or.tz");
    expect(html).toContain('target="_blank"');
    expect(html).toContain('rel="noopener noreferrer nofollow"');
  });
});

describe("email addresses", () => {
  it("links one as a mailto", () => {
    expect(linkify("write to habari@tdc.or.tz")).toContain('href="mailto:habari@tdc.or.tz"');
  });

  it("does not mistake a mention for an address", () => {
    expect(linkify("habari @amina")).not.toContain("mailto:");
  });
});

describe("phone numbers", () => {
  it("links an international number", () => {
    const html = linkify("nipigie +255 712 345 678");
    expect(html).toContain('href="tel:+255712345678"');
    // Written as it was typed, dialled without the spaces.
    expect(html).toContain(">+255 712 345 678</a>");
  });

  it("links a local number", () => {
    expect(linkify("0712 345 678")).toContain('href="tel:0712345678"');
  });

  it("links a hyphenated number", () => {
    expect(linkify("0712-345-678")).toContain('href="tel:0712345678"');
  });

  /*
   * The cases that matter more than the ones above: a pattern loose enough to
   * catch every real number also catches things that are merely digits, and a
   * conversation full of spurious tel: links is worse than one with none.
   */
  it("leaves things that are merely long numbers alone", () => {
    for (const text of [
      "order 123456789012",
      "the year 2026",
      "version 1.2.3",
      "12345",
      "1234567",
    ]) {
      expect(linkify(text)).not.toContain("tel:");
    }
  });

  it("does not claim digits inside a URL", () => {
    const html = linkify("https://example.com/2026/01/02/report-123456789");
    expect(html).not.toContain("tel:");
  });
});

describe("several in one message", () => {
  it("links each one and keeps the text between them", () => {
    const html = linkify("Tovuti tdc.or.tz, barua habari@tdc.or.tz, simu +255 712 345 678.");

    expect(html).toContain('href="https://tdc.or.tz"');
    expect(html).toContain('href="mailto:habari@tdc.or.tz"');
    expect(html).toContain('href="tel:+255712345678"');
    expect(html).toContain("Tovuti ");
    expect(html).toContain(", barua ");
  });

  it("leaves an ordinary message untouched", () => {
    expect(linkify("Habari za asubuhi, tutaonana kesho.")).toBe(
      "Habari za asubuhi, tutaonana kesho.",
    );
  });

  it("returns an empty string for an empty body", () => {
    expect(linkify("")).toBe("");
  });
});
