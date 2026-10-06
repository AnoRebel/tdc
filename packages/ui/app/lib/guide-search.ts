import type { GuideSection } from "#layers/ui/app/lib/guide-render";

/**
 * Searching the guides, once their text has been fetched.
 *
 * Every word must appear, in the heading or the text beneath it, so "rate
 * limit federation" narrows rather than widens. A section whose heading
 * matches comes first: somebody searching "backup" wants the section called
 * Backup before every paragraph that mentions one in passing.
 *
 * Plain text in and plain text out. The excerpt is drawn by the page from the
 * parts returned here, so nothing a guide contains is ever put into the page
 * as markup to show a match.
 */

export interface SectionHit {
  slug: string;
  /** The guide's own title. */
  guide: string;
  /** Empty for a match before the guide's first section heading. */
  id: string;
  heading: string;
  /** The excerpt, split so the matching words can be marked. */
  excerpt: { text: string; match: boolean }[];
}

export interface SearchableGuide {
  slug: string;
  title: string;
  sections: GuideSection[];
}

const EXCERPT = 160;

export function searchGuides(guides: SearchableGuide[], query: string, limit = 30): SectionHit[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return [];

  const scored: { hit: SectionHit; score: number }[] = [];

  for (const guide of guides) {
    for (const section of guide.sections) {
      const heading = (section.heading || guide.title).toLowerCase();
      const text = section.text.toLowerCase();
      if (!words.every((word) => heading.includes(word) || text.includes(word))) continue;

      const inHeading = words.filter((word) => heading.includes(word)).length;
      scored.push({
        score: inHeading * 10 - (section.id ? 0 : 1),
        hit: {
          slug: guide.slug,
          guide: guide.title,
          id: section.id,
          heading: section.heading || guide.title,
          excerpt: excerpt(section.text, words),
        },
      });
    }
  }

  // Stable: equal scores keep the order the guides and their sections have.
  return scored
    .map((entry, order) => ({ ...entry, order }))
    .sort((a, b) => b.score - a.score || a.order - b.order)
    .slice(0, limit)
    .map(({ hit }) => hit);
}

/** A stretch of text around the first match, with every match marked. */
export function excerpt(text: string, words: string[]): { text: string; match: boolean }[] {
  const lower = text.toLowerCase();
  const first = Math.min(
    ...words.map((word) => lower.indexOf(word)).filter((index) => index >= 0),
    text.length,
  );

  let start = Math.max(0, first - EXCERPT / 3);
  // Begin at a word rather than in the middle of one.
  if (start > 0) start = text.indexOf(" ", start) + 1 || start;
  const end = Math.min(text.length, start + EXCERPT);
  const window = text.slice(start, end);

  const pattern = new RegExp(`(${words.map(escapeRegExp).join("|")})`, "gi");
  const parts = window
    .split(pattern)
    .filter(Boolean)
    .map((part) => ({ text: part, match: words.includes(part.toLowerCase()) }));

  if (start > 0) parts.unshift({ text: "…", match: false });
  if (end < text.length) parts.push({ text: "…", match: false });
  return parts;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
