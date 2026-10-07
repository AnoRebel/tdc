import type { GuideImage } from "#layers/ui/app/lib/guide-render";
import { guideFor } from "#layers/ui/app/lib/guides";

/**
 * The guides' text, read from docs/ when the application is built.
 *
 * Each file becomes a chunk of its own, fetched only when somebody opens that
 * guide — or, for a search, once somebody starts typing one. Nothing here is in
 * the first load, and docs/ stays the only copy: editing a guide there is
 * editing what the application shows.
 *
 * A translation is a file of the same name in docs/<locale>/. None exist yet;
 * when one does, it is picked up here without a change to this file.
 */
const english = import.meta.glob<string>(["../../../../docs/*.md", "!**/README.md"], {
  query: "?raw",
  import: "default",
});

const translated = import.meta.glob<string>("../../../../docs/*/*.md", {
  query: "?raw",
  import: "default",
});

/*
 * The guides' screenshots, by their sizes.
 *
 * The images themselves are files the layer's configuration copies to
 * /guide-images, each fetched only when the reader scrolls to it. What is
 * here is docs/images/sizes.json, which apps/web/scripts/docs-screenshots.ts
 * writes beside them: it says which exist, and how much room each needs, so
 * the page can hold its place before it arrives. Globbed rather than
 * imported, so a checkout without screenshots still builds.
 */
const sizeFiles = import.meta.glob<Record<string, [number, number]>>(
  "../../../../docs/images/sizes.json",
  { import: "default", eager: true },
);
const sizes = Object.values(sizeFiles)[0] ?? {};

/**
 * A screenshot a guide refers to, by the path it is written with
 * (images/projects/board.webp, or ../images/… from a translation), or
 * undefined for one that does not exist, which the page shows as its text.
 */
export function guideImage(src: string): GuideImage | undefined {
  const name = src.replace(/^(?:\.\.?\/)+/, "").replace(/^images\//, "");
  const size = sizes[name];
  return size ? { url: `/guide-images/${name}`, width: size[0], height: size[1] } : undefined;
}

function loaderFor(slug: string, locale?: string) {
  const match = (path: string) =>
    locale ? path.endsWith(`/docs/${locale}/${slug}.md`) : path.endsWith(`/docs/${slug}.md`);
  const table = locale ? translated : english;
  const path = Object.keys(table).find(match);
  return path ? table[path] : undefined;
}

export interface LoadedGuide {
  source: string;
  /** Whether this is in the language asked for, rather than English instead. */
  translated: boolean;
}

/**
 * A guide in the reader's language where one exists, in English otherwise.
 *
 * Undefined for a slug that is not a guide, so a mistyped address is a "not
 * found" rather than an attempt to load a file nobody listed.
 */
export async function loadGuide(slug: string, locale: string): Promise<LoadedGuide | undefined> {
  if (!guideFor(slug)) return undefined;

  if (locale !== "en") {
    const local = loaderFor(slug, locale);
    if (local) return { source: await local(), translated: true };
  }

  const load = loaderFor(slug);
  if (!load) return undefined;
  return { source: await load(), translated: locale === "en" };
}

/** Whether a guide has been translated into a language, without loading it. */
export function hasTranslation(slug: string, locale: string): boolean {
  return locale === "en" || loaderFor(slug, locale) !== undefined;
}
