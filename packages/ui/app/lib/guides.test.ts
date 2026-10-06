// @vitest-environment jsdom
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { ANCHOR_PREFIX, guideSections, renderGuide } from "#layers/ui/app/lib/guide-render";
import { excerpt, searchGuides } from "#layers/ui/app/lib/guide-search";
import { createSlugger, GUIDES, resolveLink, SOURCE_REPOSITORY } from "#layers/ui/app/lib/guides";

const DOCS = join(import.meta.dirname, "..", "..", "..", "..", "docs");
const labels = { copy: "Copy", anchor: "Link to this section" };

function render(source: string) {
  const rendered = renderGuide(source, labels);
  const page = document.createElement("div");
  page.innerHTML = rendered.html;
  return { ...rendered, page };
}

describe("the guide catalogue", () => {
  it("names only files that exist in docs/", () => {
    for (const guide of GUIDES) {
      expect(existsSync(join(DOCS, `${guide.slug}.md`)), guide.slug).toBe(true);
    }
  });
});

describe("anchors", () => {
  it("are made the way a repository page makes them", () => {
    const slug = createSlugger();
    expect(slug("Signed deliveries")).toBe("signed-deliveries");
    expect(slug("The same program, with no connection held")).toBe(
      "the-same-program-with-no-connection-held",
    );
    expect(slug("`GET /api/server` and what it says")).toBe("get-apiserver-and-what-it-says");
    expect(slug("Menus, fields, and answers only you can see")).toBe(
      "menus-fields-and-answers-only-you-can-see",
    );
  });

  it("number a repeated heading rather than giving two sections one anchor", () => {
    const slug = createSlugger();
    expect([slug("Roles"), slug("Roles"), slug("Roles")]).toEqual(["roles", "roles-1", "roles-2"]);
  });

  /*
   * The guides link into each other's sections by anchor. Every one of those
   * links has to land on a heading that exists, or the help centre is a set of
   * pages whose cross-references quietly go nowhere.
   */
  it("exist for every section one guide links to in another", () => {
    const pages = new Map(
      GUIDES.map((guide) => [
        guide.slug,
        render(readFileSync(join(DOCS, `${guide.slug}.md`), "utf8")).page,
      ]),
    );

    const broken: string[] = [];
    for (const [slug, page] of pages) {
      for (const link of page.querySelectorAll<HTMLAnchorElement>("a[data-section]")) {
        const hash = link.dataset.section;
        if (!hash || link.classList.contains("guide-anchor")) continue;
        const target = link.dataset.guide ?? slug;
        // Any heading counts, including the fourth-level ones the contents leave out.
        const found = pages.get(target)?.querySelector(`#${CSS.escape(ANCHOR_PREFIX + hash)}`);
        if (!found) broken.push(`${slug} → ${target}#${hash}`);
      }
    }

    expect(broken).toEqual([]);
  }, 30_000);
});

describe("links between guides", () => {
  it("become routes inside the application", () => {
    expect(resolveLink("projects.md")).toEqual({ kind: "guide", slug: "projects", hash: "" });
    expect(resolveLink("./integrating.md#api-tokens")).toEqual({
      kind: "guide",
      slug: "integrating",
      hash: "api-tokens",
    });

    const { page } = render("See [tokens](integrating.md#api-tokens).");
    const link = page.querySelector("a")!;
    expect(link.getAttribute("href")).toBe("/help/integrating#api-tokens");
    expect(link.dataset.guide).toBe("integrating");
    expect(link.dataset.section).toBe("api-tokens");
    expect(link.hasAttribute("target")).toBe(false);
  });

  it("keep a section of the same guide in the page", () => {
    expect(resolveLink("#restore")).toEqual({ kind: "section", hash: "restore" });
  });

  it("send a file elsewhere in the repository to the source repository", () => {
    expect(resolveLink("../LICENSE")).toEqual({
      kind: "external",
      href: `${SOURCE_REPOSITORY}LICENSE`,
    });
    // README.md sits in docs/ but is not a guide here.
    expect(resolveLink("README.md")).toEqual({
      kind: "external",
      href: `${SOURCE_REPOSITORY}docs/README.md`,
    });
    expect(resolveLink("../../../etc/passwd")).toEqual({ kind: "none" });
  });

  it("open another site away from the application", () => {
    const { page } = render("[Go](https://go.dev/doc)");
    const link = page.querySelector("a")!;
    expect(link.getAttribute("href")).toBe("https://go.dev/doc");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
  });
});

describe("the table of contents", () => {
  it("lists the second and third level headings, and takes the first as the title", () => {
    const { title, toc } = render(
      "# Hosting\n\nIntro.\n\n## TLS\n\n### Behind a proxy\n\n#### Too deep\n\n## Backups\n\n```sh\n# not a heading\n```\n",
    );
    expect(title).toBe("Hosting");
    expect(toc).toEqual([
      { id: "tls", text: "TLS", depth: 2 },
      { id: "behind-a-proxy", text: "Behind a proxy", depth: 3 },
      { id: "backups", text: "Backups", depth: 2 },
    ]);
  });

  it("gives every heading in the page the anchor its entry points at", () => {
    const { page, toc } = render("# T\n\n## Getting an account\n\n## Getting an account\n");
    for (const entry of toc) {
      expect(page.querySelector(`#${ANCHOR_PREFIX}${entry.id}`), entry.id).not.toBeNull();
    }
    // The title is the page's own heading, not repeated in the body.
    expect(page.querySelector("h1")).toBeNull();
  });
});

describe("code", () => {
  it("is shown as text, with a button to copy it", () => {
    const { page } = render("```go\nif a < b && c > d {}\n```\n");
    expect(page.querySelector("pre code")!.textContent).toBe("if a < b && c > d {}");
    expect(page.querySelector(".guide-code-bar span")!.textContent).toBe("go");
    expect(page.querySelector("button[data-copy]")!.textContent).toBe("Copy");
  });
});

describe("what a guide cannot do to the page it is shown in", () => {
  it("never emits a script, whether raw, inline or in an attribute", () => {
    const { page, html } = render(
      [
        "# Hostile",
        "",
        "<script>window.ran = true</script>",
        "",
        'Inline <img src=x onerror="window.ran = true"> and <svg onload="window.ran = true"></svg>.',
        "",
        '<a href="javascript:window.ran = true">click</a>',
        "",
        "[a link](javascript:window.ran=true) and [data](data:text/html,<script>x</script>)",
        "",
        '<iframe src="https://example.com"></iframe>',
      ].join("\n"),
    );

    expect(page.querySelector("script, img, svg, iframe")).toBeNull();
    expect(html).not.toMatch(/<script/i);
    for (const element of page.querySelectorAll("*")) {
      const handlers = [...element.attributes].filter((attribute) => /^on/i.test(attribute.name));
      expect(handlers, element.tagName).toEqual([]);
    }
    for (const link of page.querySelectorAll("a")) {
      expect(link.getAttribute("href") ?? "").not.toMatch(/^\s*(javascript|data):/i);
    }
    // The raw markup is shown as what it is rather than silently vanishing.
    expect(page.textContent).toContain("<script>window.ran = true</script>");
  });

  it("drops comments, which the guides use as notes to their editors", () => {
    const { page } = render("<!-- demo: subscribe -->\n\nText.");
    expect(page.textContent?.trim()).toBe("Text.");
  });
});

describe("searching", () => {
  it("splits a guide into its sections, as text, without the code", () => {
    const { title, sections } = guideSections(
      "# Guide\n\nOpening words.\n\n## Tokens\n\nA **token** is a key.\n\n```\nAuthorization: Bearer x\n```\n\n- one\n- two\n",
    );
    expect(title).toBe("Guide");
    expect(sections).toEqual([
      { id: "", heading: "", text: "Opening words." },
      { id: "tokens", heading: "Tokens", text: "A token is a key. one two" },
    ]);
  });

  const guides = [
    {
      slug: "hosting",
      title: "Hosting",
      sections: [
        { id: "", heading: "", text: "How to run a server, and keep a backup of it." },
        { id: "backup", heading: "Backup", text: "Copy the database every night." },
        { id: "tls", heading: "TLS", text: "Certificates, renewed automatically." },
      ],
    },
  ];

  it("needs every word, and puts a section named for them first", () => {
    expect(searchGuides(guides, "backup").map((hit) => hit.id)).toEqual(["backup", ""]);
    expect(searchGuides(guides, "backup certificates")).toEqual([]);
    expect(searchGuides(guides, "   ")).toEqual([]);
  });

  it("marks the matches as text, never as markup", () => {
    const [hit] = searchGuides(
      [{ slug: "x", title: "X", sections: [{ id: "a", heading: "A", text: "<b>bold</b> claim" }] }],
      "claim",
    );
    expect(hit!.excerpt).toEqual([
      { text: "<b>bold</b> ", match: false },
      { text: "claim", match: true },
    ]);
  });

  it("shows the neighbourhood of a match deep in a long section", () => {
    const text = `${"filler ".repeat(100)}the needle is here ${"after ".repeat(100)}`;
    const parts = excerpt(text, ["needle"]);
    expect(parts[0]).toEqual({ text: "…", match: false });
    expect(parts.at(-1)).toEqual({ text: "…", match: false });
    expect(parts.some((part) => part.match && part.text === "needle")).toBe(true);
  });
});
