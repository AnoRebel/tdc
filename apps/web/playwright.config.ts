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

/*
 * A note on sign-ins, which is the thing that breaks this suite.
 *
 * The server allows eight sign-in attempts per account per quarter hour, and
 * five registrations per address per hour. Both limits are right — an endpoint
 * that accepts unlimited attempts is a password guesser's front door, and one
 * that accepts unlimited registrations is a spam account factory — so the
 * suite is what has to fit inside them.
 *
 * That is why the session is established once and everything else reads it
 * from SESSION, including the fixtures that need to act as somebody
 * privileged. A check that signs in for itself spends one of eight, and a
 * suite that does it per test reports the application as broken when what
 * broke was the suite.
 */
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
      testIgnore: /milestone-8\.spec\.ts|tour\.spec\.ts|permissions\.spec\.ts/,
    },
    /*
     * The other two engines a release is gated on.
     *
     * Chromium alone would not have caught the things these exist for: WebKit
     * is what every iPhone runs whatever browser is installed, and Firefox is
     * the one that implements a specification rather than matching Chromium's
     * reading of it. A behaviour that works in one engine is not a behaviour
     * that works.
     *
     * The accessibility pass stays on Chromium: axe reports the same
     * violations on all three, so running it three times costs minutes and
     * finds nothing.
     */
    {
      name: "firefox",
      use: { ...devices["Desktop Firefox"], storageState: SESSION },
      dependencies: ["setup"],
      testIgnore:
        /milestone-8\.spec\.ts|accessibility\.spec\.ts|tour\.spec\.ts|permissions\.spec\.ts/,
    },
    /*
     * WebKit needs libraries a Debian-family machine has and others may not —
     * libicu 74 and flite's voice libraries, by their Debian sonames. On a
     * distribution shipping a different ICU it refuses to launch, and every
     * test "fails" in milliseconds with a message about the host rather than
     * the application. `bunx playwright install --with-deps webkit` fixes it
     * where the package names exist; CI runs on ubuntu-latest, where they do.
     */
    {
      name: "webkit",
      use: { ...devices["Desktop Safari"], storageState: SESSION },
      dependencies: ["setup"],
      testIgnore:
        /milestone-8\.spec\.ts|accessibility\.spec\.ts|tour\.spec\.ts|permissions\.spec\.ts/,
    },
    /*
     * The milestone demonstration, run on its own.
     *
     * A fresh profile each time: it shows what somebody meets on a first
     * visit, and a shared session would have already been through that.
     */
    /*
     * The first-arrival checks, on a fresh profile and a fresh account.
     *
     * Separate from the engine projects because it registers its own account:
     * whether the introduction appears is a property of the account, so it
     * cannot share the signed-in one every other check reuses.
     */
    {
      name: "arrival",
      use: { ...devices["Desktop Chrome"] },
      testMatch: /tour\.spec\.ts|permissions\.spec\.ts/,
      // For the stored session, which the permission checks read to build
      // their fixture as somebody who may create a channel. The pages
      // themselves run with no session at all.
      dependencies: ["setup"],
    },
    {
      name: "milestone",
      use: { ...devices["Desktop Chrome"] },
      testMatch: /milestone-8\.spec\.ts/,
    },
  ],
});
