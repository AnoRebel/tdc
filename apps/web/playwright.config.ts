import { defineConfig, devices } from "@playwright/test";

/*
 * The accessibility checks, which need a real browser.
 *
 * jsdom can parse markup but has no layout, so it cannot say whether a control
 * is visible, what colour it resolves to, or where Tab actually goes. Those
 * are exactly the questions worth asking, so these run against a browser
 * driving the built application.
 *
 * Separate from the unit tests deliberately: those run in milliseconds on
 * every save, and these need a build and a server behind them.
 */
/*
 * Where the signed-in session is kept between the setup and the checks.
 *
 * Under test-results, which is already ignored — a file holding a session
 * cookie is not something to leave lying in the working tree.
 */
export const SESSION = "test-results/.session.json";

export default defineConfig({
  testDir: "./tests",
  // One at a time. These share a server and a signed-in account, and the
  // point is a clear answer rather than a fast one.
  workers: 1,
  fullyParallel: false,
  /*
   * No retries. A flaky accessibility failure is still a failure worth
   * looking at, and retrying until it passes is how a real violation gets
   * dismissed as noise.
   */
  retries: 0,
  reporter: process.env.CI ? "github" : "list",

  use: {
    baseURL: process.env.TDC_TEST_URL ?? "http://127.0.0.1:8099",
    // Kept only for a failure, since that is the only time anybody looks.
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [
    /*
     * Signing in once, for everything.
     *
     * A fresh browser profile per test means a fresh sign-in per test, and
     * the server rate-limits those — correctly, since a sign-in endpoint that
     * accepts unlimited attempts is a password guesser's front door. The
     * suite tripped it and then reported that the application was broken.
     *
     * So the session is established once and reused. Faster, and closer to
     * how anybody actually uses this.
     */
    { name: "setup", testMatch: /session\.setup\.ts/ },
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"], storageState: SESSION },
      dependencies: ["setup"],
      // The milestone demonstration signs itself in, because two of its four
      // parts are about what happens on arrival.
      testIgnore: /milestone-8\.spec\.ts/,
    },
    /*
     * The milestone demonstration, run on its own.
     *
     * A fresh profile each time: it shows what somebody meets on a first
     * visit, and a shared session would have already been through that.
     */
    {
      name: "milestone",
      use: { ...devices["Desktop Chrome"] },
      testMatch: /milestone-8\.spec\.ts/,
    },
  ],
});
