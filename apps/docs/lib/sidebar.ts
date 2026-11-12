/**
 * The sidebar, and the order a reader steps through the guides in.
 *
 * Nuxt Content orders pages by file name, so left alone the sidebar would be
 * alphabetical and the previous/next links under each guide would jump about
 * between groups. Both come from here instead, from the help centre's
 * catalogue (lib/guides.ts), so the links at the foot of a page always go to
 * the guide above and below it in the sidebar.
 *
 * Runs in the browser as well as the server: nothing here reads files.
 */
import type { ContentNavigationItem } from "@nuxt/content";

export interface SidebarCatalogue {
  guides: { slug: string; group: string; title: string; description: string }[];
  groups: { id: string; title: string; icon: string }[];
}

/** The guides grouped and ordered as the help centre lists them. */
export function sidebar(
  pages: ContentNavigationItem[],
  catalogue: SidebarCatalogue,
): ContentNavigationItem[] {
  const byPath = new Map(pages.map((page) => [page.path, page]));
  const placed = new Set<string>();

  const groups: ContentNavigationItem[] = catalogue.groups.map((group) => ({
    title: group.title,
    icon: group.icon,
    path: `/${group.id}`,
    children: catalogue.guides
      .filter((guide) => guide.group === group.id)
      .flatMap((guide) => {
        const page = byPath.get(`/${guide.slug}`);
        if (!page) return [];
        placed.add(page.path);
        // The short name and one-line description the help centre's index uses.
        return [{ ...page, title: guide.title, description: guide.description }];
      }),
  }));

  // A file in docs/ the help centre does not list yet still has a place here.
  const rest = pages.filter((page) => !placed.has(page.path));
  if (rest.length)
    groups.push({ title: "More", icon: "i-lucide-files", path: "/more", children: rest });

  return groups.filter((group) => group.children?.length);
}

/** Every guide, top to bottom, across the groups. */
export function readingOrder(groups: ContentNavigationItem[]): ContentNavigationItem[] {
  return groups.flatMap((group) => group.children ?? []);
}

/** The guides before and after this one in the sidebar, or null at either end. */
export function surround(
  groups: ContentNavigationItem[],
  path: string,
): [ContentNavigationItem | null, ContentNavigationItem | null] {
  const order = readingOrder(groups);
  const index = order.findIndex((page) => page.path === path);
  if (index === -1) return [null, null];
  return [order[index - 1] ?? null, order[index + 1] ?? null];
}
