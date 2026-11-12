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

/** A WebP file's width and height, read from its header. */
function webpSize(file: Buffer): [number, number] {
  const chunk = file.toString("ascii", 12, 16);
  if (chunk === "VP8 ") return [file.readUInt16LE(26) & 0x3fff, file.readUInt16LE(28) & 0x3fff];
  if (chunk === "VP8L") {
    const bits = file.readUInt32LE(21);
    return [(bits & 0x3fff) + 1, ((bits >> 14) & 0x3fff) + 1];
  }
  if (chunk === "VP8X") return [file.readUIntLE(24, 3) + 1, file.readUIntLE(27, 3) + 1];
  throw new Error(`not a WebP file (${chunk})`);
}

describe("screenshots in the guides", () => {
  const images = GUIDES.flatMap((guide) =>
    [
      ...readFileSync(join(DOCS, `${guide.slug}.md`), "utf8").matchAll(
        /!\[([^\]]*)\]\(([^)\s]+)\)/g,
      ),
    ].map(([, alt, src]) => ({ guide: guide.slug, alt: alt!, src: src! })),
  );
  const sizes = JSON.parse(readFileSync(join(DOCS, "images", "sizes.json"), "utf8")) as Record<
    string,
    [number, number]
  >;

  it("exist, and say what they show to somebody who cannot see them", () => {
    expect(images.length).toBeGreaterThan(0);
    for (const { guide, alt, src } of images) {
      expect(src, guide).toMatch(/^images\/[a-z0-9-]+\/[a-z0-9-]+\.webp$/);
      expect(existsSync(join(DOCS, src)), src).toBe(true);
      expect(alt.trim().length, `alt text for ${src}`).toBeGreaterThan(10);
    }
  });

  /*
   * The help centre reserves each image's space from sizes.json before the
   * file arrives; a size that has drifted from the file is a page that jumps.
   */
  it("have their real sizes recorded", () => {
    for (const { src } of images) {
      const name = src.slice("images/".length);
      expect(sizes[name], name).toEqual(webpSize(readFileSync(join(DOCS, src))));
    }
  });

  it("are drawn lazily at their size, and a missing one is its description", () => {
    const lookup = (src: string) =>
      src === "images/a/b.webp" ? { url: "/_nuxt/b.webp", width: 1280, height: 800 } : undefined;
    const rendered = renderGuide(
      "![The board, by state](images/a/b.webp)\n\n![Nothing](images/a/gone.webp)",
      labels,
      lookup,
    );
    const page = document.createElement("div");
    page.innerHTML = rendered.html;
    const image = page.querySelector("img")!;
    expect(image.getAttribute("src")).toBe("/_nuxt/b.webp");
    expect(image.getAttribute("alt")).toBe("The board, by state");
    expect(image.getAttribute("loading")).toBe("lazy");
    expect([image.getAttribute("width"), image.getAttribute("height")]).toEqual(["1280", "800"]);
    expect(page.querySelectorAll("img")).toHaveLength(1);
    expect(page.textContent).toContain("Nothing");
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

/*
 * The public documentation site edits these files through Studio, which saves
 * a title and description as front matter. The help centre reads the same
 * files, and has to read them as the guide they are.
 */
describe("front matter", () => {
  const edited =
    "---\ntitle: Hosting a TDC server\ndescription: Everything needed.\n---\n\n# Hosting a TDC server\n\nEverything needed.\n\n## TLS\n\nText.\n";

  it("is not shown, and the guide's own heading stays its title", () => {
    const { title, toc, page } = render(edited);
    expect(title).toBe("Hosting a TDC server");
    expect(toc).toEqual([{ id: "tls", text: "TLS", depth: 2 }]);
    expect(page.querySelector("hr")).toBeNull();
    expect(page.textContent).not.toContain("description:");
  });

  it("is not searched", () => {
    const { sections } = guideSections(edited);
    expect(sections.map((section) => section.text).join(" ")).not.toContain("title:");
  });

  it("is only a block at the very top, so a rule further down is left alone", () => {
    const { page } = render("# Guide\n\nAbove.\n\n---\n\nBelow.\n\n---\n");
    expect(page.querySelectorAll("hr")).toHaveLength(2);
  });
});

/*
 * Studio can insert Nuxt Content's component syntax. The help centre would
 * print it as text, so the guides stay plain GitHub-flavoured markdown.
 */
describe("the guides as written", () => {
  it("use no component blocks, which only the documentation site understands", () => {
    const offenders: string[] = [];
    for (const guide of GUIDES) {
      const source = readFileSync(join(DOCS, `${guide.slug}.md`), "utf8");
      let fenced = false;
      source.split("\n").forEach((line, index) => {
        if (/^\s*(```|~~~)/.test(line)) fenced = !fenced;
        if (!fenced && /^\s*::+[a-z]/i.test(line)) offenders.push(`${guide.slug}.md:${index + 1}`);
      });
    }
    expect(offenders).toEqual([]);
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
