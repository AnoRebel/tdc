import { defineConfig, devices } from "@playwright/test";

/*
 * The projects milestones, demonstrated in a real browser.
 *
 * Kept apart from the suite in tests/ because these are run deliberately, by
 * the scripts/demo-milestone-projects-*.sh scripts, against a server each
 * script starts on a database of its own. They sign in for themselves and make
 * everything they touch, so they depend on no stored session and leave the
 * suite's fixtures alone.
 */
export default defineConfig({
  testDir: "./milestones",
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  // Generous: a demonstration signs several people in and waits on live
  // delivery between browsers, and a slow machine is not a failed milestone.
  timeout: 300_000,
  use: {
    baseURL: process.env.TDC_TEST_URL ?? "http://localhost:8401",
    // A step that cannot happen says so, rather than waiting out the test.
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "desktop",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
