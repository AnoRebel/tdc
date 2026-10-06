import { describe, expect, it } from "bun:test";

import { prepareGuide, remarkGuideLinks, siteHref, type ParsedBody } from "./guides";
import { redirectStudioRequest } from "./studio-paths";

describe("links in a guide, on the site", () => {
  it("go to the other guide's page, keeping the section", () => {
    expect(siteHref("hosting.md#tls", "dev")).toBe("/hosting#tls");
    expect(siteHref("./projects.md", "dev")).toBe("/projects");
    expect(siteHref("#roles", "dev")).toBe("#roles");
  });

  it("open other repository files on the branch the site was built from", () => {
    expect(siteHref("../SECURITY.md", "beta")).toBe(
      "https://github.com/AnoRebel/tdc/blob/beta/SECURITY.md",
    );
    expect(siteHref("README.md", "main")).toBe(
      "https://github.com/AnoRebel/tdc/blob/main/docs/README.md",
    );
  });

  it("go nowhere when there is nowhere safe to go", () => {
    expect(siteHref("javascript:alert(1)", "dev")).toBeNull();
  });

  it("are rewritten as written, before anything strips the extension", () => {
    const tree = {
      type: "root",
      children: [
        {
          type: "paragraph",
          children: [
            { type: "link", url: "upgrading.md#rolling-back", children: [] },
            { type: "link", url: "javascript:x", children: [{ type: "text", value: "words" }] },
          ],
        },
      ],
    };
    remarkGuideLinks({ branch: "dev" })(tree);
    const paragraph = tree.children[0]!.children as { type: string; url?: string }[];
    expect(paragraph[0]).toMatchObject({ type: "link", url: "/upgrading#rolling-back" });
    expect(paragraph[1]).toMatchObject({ type: "text", value: "words" });
  });
});

describe("a guide's page", () => {
  it("gets the anchors a repository page gives its headings, and drops the repeated title", () => {
    const body: ParsedBody = {
      value: [
        ["h1", { id: "hosting" }, "Hosting"],
        ["p", {}, "Intro."],
        ["h2", { id: "whats-new" }, "What's ", ["code", {}, "new"], "?"],
        ["h2", { id: "whats-new-1" }, "What's ", ["code", {}, "new"], "?"],
      ],
      toc: { links: [{ id: "whats-new" }, { id: "whats-new-1" }] },
    };
    prepareGuide(body);
    expect(body.value!.map((node) => (node as [string, { id: string }])[1].id)).toEqual([
      "whats-new",
      "whats-new-1",
    ]);
    expect(body.toc!.links!.map((link) => link.id)).toEqual(["whats-new", "whats-new-1"]);
  });
});

describe("Studio's requests", () => {
  const repo = "AnoRebel/tdc";
  const api = "https://api.github.com/repos/AnoRebel/tdc";

  it("read a guide from docs/ rather than from the app's content/", () => {
    expect(
      redirectStudioRequest(`${api}/contents/apps/docs/content/hosting.md?ref=dev`, undefined, repo)
        .url,
    ).toBe(`${api}/contents/docs/hosting.md?ref=dev`);
  });

  it("commit a guide to docs/, deletions included", () => {
    const { body } = redirectStudioRequest(
      `${api}/git/trees`,
      JSON.stringify({
        base_tree: "abc",
        tree: [
          { path: "apps/docs/content/hosting.md", sha: "1" },
          { path: "apps/docs/content/old.md", sha: null },
          { path: "apps/docs/public/picture.png", sha: "2" },
        ],
      }),
      repo,
    );
    expect(JSON.parse(body as string).tree.map((entry: { path: string }) => entry.path)).toEqual([
      "docs/hosting.md",
      "docs/old.md",
      "apps/docs/public/picture.png",
    ]);
  });

  it("leave every other request alone", () => {
    const other = { url: "https://api.github.com/repos/someone/else/git/trees", body: "{}" };
    expect(redirectStudioRequest(other.url, other.body, repo)).toEqual(other);
    expect(redirectStudioRequest(`${api}/git/commits`, "{}", repo)).toEqual({
      url: `${api}/git/commits`,
      body: "{}",
    });
  });
});
