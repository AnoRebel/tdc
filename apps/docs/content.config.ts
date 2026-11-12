import { defineCollection, defineContentConfig, z } from "@nuxt/content";

import { DOCS_DIR } from "./lib/guides";

/**
 * The guides, read from the repository's docs/ directory where the application
 * reads them too. There is no content/ directory in this app on purpose: a copy
 * here would be a second version of every guide, and the one somebody forgot
 * to update would be the one people read.
 *
 * Replaces the layer's own "docs" collection, which looks in content/. README
 * is the repository's index of these files rather than a guide, as it is for
 * the help centre; the site's front page takes its place.
 */
export default defineContentConfig({
  collections: {
    docs: defineCollection({
      type: "page",
      source: {
        cwd: DOCS_DIR,
        include: "*.md",
        exclude: ["README.md"],
        prefix: "/",
      },
      // The layer's page template reads this; the guides never set it.
      schema: z.object({
        links: z
          .array(
            z.object({
              label: z.string(),
              icon: z.string(),
              to: z.string(),
              target: z.string().optional(),
            }),
          )
          .optional(),
      }),
    }),
  },
});
