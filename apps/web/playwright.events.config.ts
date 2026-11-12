import { defineConfig, devices } from "@playwright/test";

/*
 * The events milestones, demonstrated in a real browser.
 *
 * Run deliberately, by the scripts/demo-milestone-events-*.sh scripts, each
 * against a server it starts on a database of its own. The people a spec needs
 * are made by its script and handed over in a file (TDC_PEOPLE_FILE); the
 * spec signs in for itself and depends on no stored session.
 *
 * Chromium has fake camera and microphone throughout: the third milestone holds
 * a call, and the flags change nothing for the others.
 */
export default defineConfig({
  testDir: "./milestones",
  testMatch: /events-milestone-\d+\.spec\.ts/,
  // Apart from what other suites write, which may be running beside this one.
  outputDir: `./test-results/events-milestones/${process.env.TDC_DEMO_NAME ?? "run"}`,
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  // Generous: one demonstration waits for a reminder the server's minute pass
  // sends, and a slow machine is not a failed milestone.
  timeout: 600_000,
  // A page reading twenty people's answers on a busy machine takes a moment.
  expect: { timeout: 30_000 },
  use: {
    baseURL: process.env.TDC_TEST_URL ?? "http://localhost:8501",
    actionTimeout: 30_000,
    navigationTimeout: 30_000,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "events",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: {
          args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"],
        },
      },
    },
  ],
});
