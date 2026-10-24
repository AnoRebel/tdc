// @vitest-environment jsdom
import { describe, expect, it } from "vitest";

import { highlighterLanguage, nameMentions, renderMessage } from "./render-message";

const render = (body: string) => renderMessage(body, { copy: "Copy" });

/** Every tag in some markup, by name. */
function tagsIn(html: string): string[] {
  return [...html.matchAll(/<([a-z0-9]+)/gi)].map((match) => match[1]!.toLowerCase());
}

/*
 * The renderer may only ever produce its own small vocabulary, plus the code
 * block's header it builds itself. Whatever a message says, nothing else.
 */
const PRODUCIBLE = new Set([
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
  "div",
  "span",
  "button",
]);

describe("what a message can never become", () => {
  const attempts = [
    "<script>alert(1)</script>",
    "**bold** <script>alert(1)</script> https://tdc.or.tz",
    "<img src=x onerror=alert(1)>",
    '<a href="javascript:alert(1)">click</a>',
    "[click](javascript:alert(1))",
    "[click](JaVaScRiPt:alert(1))",
    "[click](  javascript:alert(1) )",
    "[click](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)",
    "![x](javascript:alert(1))",
    "![x](https://evil.example/track.png)",
    "<iframe src=https://evil.example></iframe>",
    "<style>body{display:none}</style>",
    "```html\n<script>alert(1)</script>\n```",
    "`<img src=x onerror=alert(1)>`",
    "> <svg onload=alert(1)>",
    "- <b onmouseover=alert(1)>x</b>",
    '[x](https://tdc.or.tz "a\\" onmouseover=\\"alert(1)")',
    "&lt;script&gt;alert(1)&lt;/script&gt;",
    "<!-- comment --> after",
    "# <script>alert(1)</script>",
  ];

  for (const attempt of attempts) {
    it(`keeps ${JSON.stringify(attempt)} inert`, () => {
      const html = render(attempt);
      for (const tag of tagsIn(html)) expect(PRODUCIBLE, `${tag} in ${html}`).toContain(tag);
      /*
       * Checked on the parsed result rather than the string: escaped text may
       * well contain "onerror=" as characters of a sentence, which is inert.
       */
      const holder = document.createElement("div");
      holder.innerHTML = html;
      for (const element of holder.querySelectorAll("*")) {
        for (const attribute of element.attributes) {
          expect(attribute.name, html).not.toMatch(/^on/i);
          expect(attribute.name, html).not.toBe("style");
          expect(attribute.name, html).not.toBe("src");
        }
      }
      for (const link of holder.querySelectorAll("a")) {
        expect(link.getAttribute("href"), html).toMatch(/^(https?:|mailto:|tel:)/);
        expect(link.getAttribute("rel"), html).toContain("noopener");
      }
    });
  }

  it("shows raw HTML as the text somebody typed", () => {
    const holder = document.createElement("div");
    holder.innerHTML = render("<b>not bold</b>");
    expect(holder.textContent).toBe("<b>not bold</b>");
    expect(holder.querySelector("b")).toBeNull();
  });

  it("decodes an entity as Markdown does, and still never as markup", () => {
    const holder = document.createElement("div");
    holder.innerHTML = render("&lt;script&gt; &amp; more");
    expect(holder.textContent).toBe("<script> & more");
    expect(holder.querySelector("script")).toBeNull();
  });

  it("refuses a Markdown link to anything but the web and mail", () => {
    expect(render("[click](javascript:alert(1))")).not.toContain("<a");
    expect(render("[docs](https://tdc.or.tz)")).toContain('href="https://tdc.or.tz"');
    expect(render("[write](mailto:a@tdc.or.tz)")).toContain('href="mailto:a@tdc.or.tz"');
  });

  it("makes an image a link to its address rather than fetching it", () => {
    const html = render("![a diagram](https://tdc.or.tz/a.png)");
    expect(html).not.toContain("<img");
    expect(html).toContain('<a href="https://tdc.or.tz/a.png"');
    expect(html).toContain("a diagram");
  });
});

describe("what renders", () => {
  it("bold, italic, strikethrough and inline code", () => {
    expect(render("**b** _i_ *i* ~~s~~ `c`")).toBe(
      "<strong>b</strong> <em>i</em> <em>i</em> <del>s</del> <code>c</code>",
    );
  });

  it("emphasis nested inside emphasis", () => {
    expect(render("**bold _and italic_**")).toBe("<strong>bold <em>and italic</em></strong>");
  });

  it("leaves a single tilde and snake_case alone", () => {
    expect(render("~5 minutes, see my_var_name")).toBe("~5 minutes, see my_var_name");
  });

  it("a fenced code block, with its language and a copy button", () => {
    const html = render("```go\nfunc main() {}\n```");
    const holder = document.createElement("div");
    holder.innerHTML = html;

    expect(holder.querySelector(".code-block-bar span")?.textContent).toBe("go");
    expect(holder.querySelector("[data-copy-code]")?.textContent).toBe("Copy");
    expect(holder.querySelector("pre code.language-go")?.textContent).toBe("func main() {}");
  });

  it("keeps a language label from becoming an attribute", () => {
    const html = render('```js" onclick="alert(1)\nx\n```');
    expect(html).not.toContain("onclick");
  });

  it("a quote and both kinds of list", () => {
    const html = render("> quoted\n\n- one\n- two\n\n3. three\n4. four");
    expect(html).toContain("<blockquote>");
    expect(html).toContain("<ul><li>one</li><li>two</li></ul>");
    expect(html).toContain('<ol start="3"><li>three</li><li>four</li></ol>');
  });

  it("a heading as a bold line rather than a title", () => {
    expect(render("# Release notes\nmore")).toContain("<strong>Release notes</strong>");
    expect(render("# Release notes")).not.toContain("<h1");
  });

  it("leaves what people type by accident as text", () => {
    expect(render("a line\n---")).not.toContain("<h2");
    expect(render("    indented")).not.toContain("<pre");
    expect(render("| a | b |\n|---|---|\n| 1 | 2 |")).not.toContain("<table");
  });

  it("a plain message exactly as it always was: text with its line breaks", () => {
    expect(render("hello\nthere")).toBe("hello\nthere");
    expect(render("👋")).toBe("👋");
  });
});

describe("links, found as they always were", () => {
  it("links an address, an email and a phone number in the text", () => {
    const html = render("see tdc.or.tz or amina@tdc.or.tz, or +255 712 345 678");
    expect(html).toContain('href="https://tdc.or.tz"');
    expect(html).toContain('href="mailto:amina@tdc.or.tz"');
    expect(html).toContain('href="tel:+255712345678"');
    expect(html).toContain('rel="noopener noreferrer nofollow"');
  });

  it("links inside emphasis", () => {
    expect(render("**https://tdc.or.tz**")).toContain(
      '<strong><a href="https://tdc.or.tz" target="_blank"',
    );
  });

  it("never links anything inside code", () => {
    expect(render("`https://tdc.or.tz`")).toBe("<code>https://tdc.or.tz</code>");
    expect(render("```\ncurl https://tdc.or.tz\n```")).not.toContain("<a");
  });

  it("leaves mentions and channel names as the text they are, in code or out", () => {
    expect(render("@amina see #general")).toBe("@amina see #general");
    expect(render("`@amina #general`")).toBe("<code>@amina #general</code>");
  });
});

describe("naming a language for the highlighter", () => {
  it("maps what people write after a fence", () => {
    expect(highlighterLanguage("TypeScript")).toBe("ts");
    expect(highlighterLanguage("golang")).toBe("go");
    expect(highlighterLanguage("brainfreeze")).toBeNull();
  });
});

describe("nameMentions", () => {
  const names: Record<string, string> = { amina: "Amina Juma", baraka: "baraka" };
  const named = (html: string) => nameMentions(html, (username) => names[username]);

  it("draws a mention as the person's name, keeping the username on hover", () => {
    const html = named(render("thanks @amina!"));
    expect(html).toContain('<span class="mention" title="@amina">Amina Juma</span>');
    expect(html).not.toContain("@amina!");
  });

  it("leaves a mention of somebody without a display name, or unknown, as typed", () => {
    expect(named(render("ask @baraka or @zawadi"))).toBe(render("ask @baraka or @zawadi"));
  });

  it("does not touch code, links or email addresses", () => {
    const body = "`@amina` and amina@example.com";
    expect(named(render(body))).not.toContain("mention");
  });

  it("sets a name as text, never markup", () => {
    const html = nameMentions(render("hi @amina"), () => "<b>x</b>");
    expect(html).toContain("&lt;b&gt;x&lt;/b&gt;");
  });
});
