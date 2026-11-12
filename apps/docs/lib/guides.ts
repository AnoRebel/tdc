/**
 * What the site knows about the guides, taken from where the application
 * already keeps it.
 *
 * The help centre inside the application decides which files in docs/ are
 * guides, which group each belongs to, what each is called on its index, and
 * how a link inside one resolves (packages/ui/app/lib/guides.ts and the
 * English translations). The site reads the same answers rather than keeping
 * its own list, so a guide added to the help centre appears here in the same
 * place without anybody remembering a second file.
 *
 * Read when the site is built — nothing here reaches the browser.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  createSlugger,
  GUIDES,
  guideKey,
  resolveLink,
  SOURCE_REPOSITORY,
  type GuideGroup,
} from "../../../packages/ui/app/lib/guides";

/** The repository's docs/ directory: the only copy of the guides. */
export const DOCS_DIR = fileURLToPath(new URL("../../../docs", import.meta.url));

export interface SiteGuide {
  slug: string;
  group: GuideGroup;
  /** The short name the help centre's index uses; the page keeps its own heading. */
  title: string;
  description: string;
}

export interface SiteGroup {
  id: GuideGroup;
  title: string;
  description: string;
  icon: string;
}

interface HelpStrings {
  groups: Record<string, string>;
  guides: Record<string, { title: string; description: string }>;
}

function helpStrings(): HelpStrings {
  const path = fileURLToPath(new URL("../../../packages/ui/locales/en.json", import.meta.url));
  return (JSON.parse(readFileSync(path, "utf8")) as { help: HelpStrings }).help;
}

/*
 * Named for a public site rather than for somebody already signed in to one:
 * "Using it" reads well under a server's own Help menu and oddly here.
 */
const GROUP_TITLES: Record<GuideGroup, { title: string; icon: string }> = {
  using: { title: "Using TDC", icon: "i-lucide-messages-square" },
  operators: { title: "Operators", icon: "i-lucide-server" },
  developers: { title: "Developers", icon: "i-lucide-code" },
  templates: { title: "Policy templates", icon: "i-lucide-scale" },
};

export function siteCatalogue(): { guides: SiteGuide[]; groups: SiteGroup[] } {
  const help = helpStrings();
  const guides = GUIDES.map((guide) => {
    const strings = help.guides[guideKey(guide.slug)];
    return {
      slug: guide.slug,
      group: guide.group,
      title: strings?.title ?? guide.slug,
      description: strings?.description ?? "",
    };
  });
  const groups = (Object.keys(GROUP_TITLES) as GuideGroup[]).map((id) => ({
    id,
    ...GROUP_TITLES[id],
    description: help.groups[`${id}Hint`] ?? "",
  }));
  return { guides, groups };
}

/* ------------------------------------------------------------------------- */

/** A node of a parsed page in Nuxt Content's compact form: [tag, props, ...children]. */
type Node = string | [string, Record<string, unknown>, ...Node[]];

interface TocLink {
  id: string;
  children?: TocLink[];
}

export interface ParsedBody {
  value?: Node[];
  toc?: { links?: TocLink[] };
}

function textOf(node: Node): string {
  if (typeof node === "string") return node;
  if (!Array.isArray(node)) return "";
  return node
    .slice(2)
    .map((child) => textOf(child as Node))
    .join("");
}

/**
 * Readies a parsed guide for the site.
 *
 * Headings get the anchors GitHub and the help centre give them. The guides
 * link into each other's sections by those anchors and were written against
 * them; Nuxt Content's own differ for headings with punctuation, and the links
 * would quietly stop landing.
 *
 * Done on the parsed page rather than on the file, so the markdown in docs/
 * stays exactly what the application reads.
 */
export function prepareGuide(body: ParsedBody): void {
  const slug = createSlugger();
  const ids = new Map<string, string>();

  const visit = (node: Node) => {
    if (!Array.isArray(node)) return;
    const [tag, props] = node;
    if (/^h[1-6]$/.test(tag)) {
      const id = slug(textOf(node));
      if (typeof props.id === "string") ids.set(props.id, id);
      props.id = id;
    }
    for (const child of node.slice(2)) visit(child as Node);
  };

  for (const node of body.value ?? []) visit(node);

  /*
   * The page header already shows the guide's heading and its first paragraph
   * (Nuxt Content takes them as title and description), so the body starts
   * after them rather than repeating both.
   */
  const value = body.value ?? [];
  const firstElement = () => value.findIndex((node) => Array.isArray(node));
  const tagAt = (index: number) => (index === -1 ? undefined : value[index]?.[0]);
  if (tagAt(firstElement()) === "h1") {
    value.splice(firstElement(), 1);
    if (tagAt(firstElement()) === "p") value.splice(firstElement(), 1);
  }

  const renameToc = (links: TocLink[] | undefined) => {
    for (const link of links ?? []) {
      link.id = ids.get(link.id) ?? link.id;
      renameToc(link.children);
    }
  };
  renameToc(body.toc?.links);
}

/**
 * Where a link written in a guide goes on the site, or null for a link with
 * nowhere safe to go (the words stay, the link does not).
 *
 * The same resolution the help centre uses: another guide becomes that page,
 * a section stays a section, and a file elsewhere in the repository opens on
 * GitHub — on the branch this site was built from, so the development docs
 * point at development sources.
 */
export function siteHref(href: string, branch: string): string | null {
  const link = resolveLink(href);
  switch (link.kind) {
    case "guide":
      return `/${link.slug}${link.hash ? `#${link.hash}` : ""}`;
    case "section":
      return `#${link.hash}`;
    case "external":
      return link.href.startsWith(SOURCE_REPOSITORY)
        ? SOURCE_REPOSITORY.replace(/\/blob\/[^/]+\/$/, `/blob/${branch}/`) +
            link.href.slice(SOURCE_REPOSITORY.length)
        : link.href;
    case "none":
      return null;
  }
}

/**
 * Where a screenshot written in a guide is served on the site.
 *
 * The guides name them relative to docs/ (images/projects/board.webp), which
 * is right on a repository page and wrong under a page's own address —
 * /projects/images/… — so they are served from /images and named from there.
 * Anything else is left as written.
 */
export function siteImage(url: string): string {
  const path = url.replace(/^(?:\.\.?\/)+/, "");
  return path.startsWith("images/") ? `/${path}` : url;
}

interface MdastNode {
  type: string;
  url?: string;
  children?: MdastNode[];
}

/**
 * A remark plugin applying siteHref to every link as written, and siteImage
 * to every image.
 *
 * It has to see the link before Nuxt Content does: Content turns "hosting.md"
 * into "hosting" and "../SECURITY.md" into "../security", after which a guide,
 * a repository file and a missing extension can no longer be told apart.
 */
export function remarkGuideLinks(options: { branch: string }) {
  const visit = (node: MdastNode) => {
    const children = node.children;
    if (!children) return;
    for (let index = 0; index < children.length; index++) {
      const child = children[index]!;
      if (child.type === "image" && child.url !== undefined) child.url = siteImage(child.url);
      if ((child.type === "link" || child.type === "definition") && child.url !== undefined) {
        const href = siteHref(child.url, options.branch);
        if (href === null && child.type === "link") {
          children.splice(index, 1, ...(child.children ?? []));
          index--;
          continue;
        }
        child.url = href ?? "";
      }
      visit(child);
    }
  };
  return (tree: MdastNode) => visit(tree);
}
