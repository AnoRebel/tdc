/**
 * The guides the help centre can show, and how links between them resolve.
 *
 * The guides themselves are the files in docs/ — read at build time, never
 * copied. A copy would be a second version that drifts from the first, and the
 * one people read in the application would be the one nobody remembered to
 * update. This file says only which of those files are guides, where each
 * belongs on the index, and what a link inside one means once it is shown
 * inside the application rather than on a repository page.
 *
 * Nothing here imports the renderer or the content, so the sidebar, the
 * command palette and the index can name the guides without pulling either
 * into what they load.
 */
import type { Component } from "vue";
import {
  ArrowRightLeft,
  CircleArrowUp,
  Contact,
  GitPullRequest,
  Kanban,
  MessagesSquare,
  Network,
  Plug,
  Scale,
  Server,
  Shield,
  Webhook,
} from "lucide-vue-next";

/** Where a guide sits on the help index. */
export type GuideGroup = "using" | "operators" | "developers" | "templates";

export interface Guide {
  /** The file's name without .md, which is also its address in the app. */
  slug: string;
  group: GuideGroup;
  /*
   * A component rather than a name for the icon bundle: these are drawn only
   * on the help pages, and a name would put every one of them into the bundle
   * every page fetches ahead.
   */
  icon: Component;
}

/**
 * Every guide, in the order the index shows them.
 *
 * Listed rather than discovered: a new file in docs/ is not automatically
 * something the people using a server should be shown. README.md is the
 * repository's index of these and is deliberately absent.
 */
export const GUIDES: readonly Guide[] = [
  { slug: "using-tdc", group: "using", icon: MessagesSquare },
  { slug: "moving-from-telegram-and-whatsapp", group: "using", icon: ArrowRightLeft },
  { slug: "projects", group: "using", icon: Kanban },
  { slug: "directory", group: "using", icon: Contact },
  { slug: "hosting", group: "operators", icon: Server },
  { slug: "upgrading", group: "operators", icon: CircleArrowUp },
  { slug: "integrating", group: "developers", icon: Plug },
  { slug: "webhooks-and-apps", group: "developers", icon: Webhook },
  { slug: "federation-protocol", group: "developers", icon: Network },
  { slug: "contributing", group: "developers", icon: GitPullRequest },
  { slug: "privacy-policy", group: "templates", icon: Shield },
  { slug: "terms-of-use", group: "templates", icon: Scale },
];

const bySlug = new Map(GUIDES.map((guide) => [guide.slug, guide]));

export function guideFor(slug: string): Guide | undefined {
  return bySlug.get(slug);
}

/**
 * Where a guide's name and description are kept in the translations:
 * "webhooks-and-apps" is help.guides.webhooksAndApps.
 *
 * The name on the index is the interface's to translate even while the guide
 * itself is English only — somebody choosing which guide to open should be
 * able to read the choice.
 */
export function guideKey(slug: string): string {
  return slug.replace(/-([a-z])/g, (_, letter: string) => letter.toUpperCase());
}

/**
 * Where the repository's own files are read, for a guide linking to one.
 *
 * A guide that says "see the licence" links to ../LICENSE, which on a
 * repository page is a file beside it. Inside the application that path means
 * nothing, so it is sent to the same file in the source repository.
 */
export const SOURCE_REPOSITORY = "https://github.com/AnoRebel/tdc/blob/main/";

/**
 * A heading's anchor, the way a repository page makes one.
 *
 * The guides link to each other's sections by these — integrating.md#api-tokens
 * — and they were written against how GitHub renders them. Producing the same
 * anchors is what keeps every one of those links working in the application
 * without anybody having to rewrite a guide for it: lower case, punctuation
 * dropped, spaces become hyphens, and a repeated heading gains -1, -2.
 */
export function createSlugger() {
  const seen = new Map<string, number>();

  return function slug(text: string): string {
    const base = text
      .trim()
      .toLowerCase()
      .replace(/[^\p{L}\p{M}\p{N}\p{Pc} -]/gu, "")
      .replace(/ /g, "-");

    const count = seen.get(base);
    seen.set(base, (count ?? -1) + 1);
    return count === undefined ? base : `${base}-${count + 1}`;
  };
}

/** What a link in a guide turns out to point at. */
export type ResolvedLink =
  /** Another guide, or a section of one, shown inside the application. */
  | { kind: "guide"; slug: string; hash: string }
  /** A section of the guide being read. */
  | { kind: "section"; hash: string }
  /** Somewhere else entirely, opened away from the application. */
  | { kind: "external"; href: string }
  /** Nothing safe to link to; the text is shown without a link. */
  | { kind: "none" };

/**
 * Resolves a link as written in a guide.
 *
 * Only http, https and mailto leave the application. Anything else with a
 * scheme — javascript:, data:, file: — is not a link a guide has any reason to
 * contain, so it is dropped rather than reasoned about; the sanitiser would
 * refuse most of them anyway, and this means it never has to.
 */
export function resolveLink(href: string): ResolvedLink {
  const target = href.trim();
  if (!target) return { kind: "none" };

  if (target.startsWith("#")) {
    return { kind: "section", hash: decodeHash(target.slice(1)) };
  }

  const scheme = /^([a-z][a-z0-9+.-]*):/i.exec(target)?.[1]?.toLowerCase();
  if (scheme) {
    return scheme === "http" || scheme === "https" || scheme === "mailto"
      ? { kind: "external", href: target }
      : { kind: "none" };
  }

  // Protocol-relative: another host, written without saying how to reach it.
  if (target.startsWith("//")) return { kind: "external", href: `https:${target}` };

  const [path = "", hash = ""] = target.split("#", 2);

  // A guide beside this one: "projects.md", "./projects.md".
  const sibling = /^(?:\.\/)?([a-z0-9-]+)\.md$/i.exec(path)?.[1];
  if (sibling && guideFor(sibling)) {
    return { kind: "guide", slug: sibling, hash: decodeHash(hash) };
  }

  /*
   * A file elsewhere in the repository. Paths in docs/ are relative to docs/,
   * so ../LICENSE is the repository root's LICENSE and a bare name is a file
   * in docs/ that is not a guide here — README.md, for one.
   */
  const fromRoot = normalise(`docs/${path}`);
  if (fromRoot === null) return { kind: "none" };
  return {
    kind: "external",
    href: `${SOURCE_REPOSITORY}${fromRoot}${hash ? `#${hash}` : ""}`,
  };
}

/** Collapses ./ and ../ segments, or null for a path that climbs out of the root. */
function normalise(path: string): string | null {
  const parts: string[] = [];
  for (const part of path.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (!parts.length) return null;
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  return parts.join("/");
}

function decodeHash(hash: string): string {
  try {
    return decodeURIComponent(hash);
  } catch {
    return hash;
  }
}
