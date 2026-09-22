import { fileURLToPath } from "node:url";

import { defineConfig } from "vitest/config";

/**
 * The layer's own tests, runnable from the layer.
 *
 * These tests are also picked up by the web application's run, which is where
 * CI executes them. This exists because running vitest from inside this
 * package without it does something worse than refusing: the files that import
 * through the #layers/ui alias fail to load, and the run reports two failing
 * test files and a passing rest — which reads as a real regression rather than
 * a missing alias, and costs somebody an afternoon proving otherwise.
 */
export default defineConfig({
  test: {
    include: ["app/**/*.test.ts", "locales/**/*.test.ts"],
    exclude: ["**/node_modules/**", "**/.output/**"],
  },

  resolve: {
    alias: {
      /*
       * Nuxt resolves this at build time; a test process does not, so a file
       * in the layer importing its own sibling fails to load. Pointed at the
       * layer itself, which is what the alias means.
       */
      "#layers/ui": fileURLToPath(new URL(".", import.meta.url)),
    },
  },
});
