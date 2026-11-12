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
      testIgnore: /project-federation\.spec\.ts|calls-throttled\.spec\.ts/,
    },
    /*
     * A call on a connection that is throttled and then freed. Run by
     * scripts/demo-milestone-calls-7.sh inside a network namespace of its own,
     * where the spec may throttle loopback with tc. Addresses are offered as
     * they are: nothing in the namespace answers the multicast that would
     * resolve the names Chromium otherwise hides them behind.
     */
    {
      name: "calls-throttled",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: {
          args: [
            "--use-fake-device-for-media-stream",
            "--use-fake-ui-for-media-stream",
            "--disable-features=WebRtcHideLocalIpsWithMdns",
          ],
        },
      },
      testMatch: /calls-throttled\.spec\.ts/,
    },
    /*
     * A project shared between two servers, in each engine a release is gated
     * on. Run by scripts/demo-milestone-project-federation-5.sh, which starts
     * both servers afresh for each engine: the last check stops one of them.
     */
    ...(["chromium", "firefox", "webkit"] as const).map((engine) => ({
      name: `federation-${engine}`,
      use: {
        ...devices[
          engine === "chromium"
            ? "Desktop Chrome"
            : engine === "firefox"
              ? "Desktop Firefox"
              : "Desktop Safari"
        ],
      },
      testMatch: /project-federation\.spec\.ts/,
    })),
  ],
});
