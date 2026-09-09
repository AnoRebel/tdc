import { defineConfig } from "vitest/config";

/**
 * The unit tests, which run in milliseconds on every save.
 *
 * Separated from the browser checks by name rather than by directory: those
 * live in tests/ and are driven by Playwright against a built application and
 * a running server. Vitest picking them up runs them with no browser at all,
 * which fails in a way that says nothing about either.
 */
export default defineConfig({
  test: {
    include: ["app/**/*.test.ts", "../../packages/**/*.test.ts"],
    exclude: ["**/node_modules/**", "**/.output/**", "tests/**"],

    /*
     * Transforms kept between runs.
     *
     * Nearly forty per cent of a run was re-transforming modules that had not
     * changed. These are meant to be fast enough to run on every save, and a
     * suite people wait for is a suite people stop running.
     */
    fsModuleCache: true,
  },
});
