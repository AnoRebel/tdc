import { defineConfig, devices } from "@playwright/test";

/*
 * Calls Milestones 1, 5 and 6, demonstrated in real browsers.
 *
 * Run deliberately by scripts/demo-milestone-calls-{1,5,6}.sh, each against a
 * server it starts on a database of its own; Milestones 1 and 6 run inside a
 * network namespace of their own (unshare, no root), where addresses are
 * offered as they are: nothing there answers the multicast that would resolve
 * the names Chromium otherwise hides them behind.
 */
const fakeMedia = [
  "--use-fake-device-for-media-stream",
  "--use-fake-ui-for-media-stream",
  "--disable-features=WebRtcHideLocalIpsWithMdns",
];

export default defineConfig({
  testDir: "./milestones",
  testMatch: /calls-milestone-[156]\.spec\.ts/,
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  timeout: 300_000,
  use: {
    baseURL: process.env.TDC_TEST_URL ?? "http://localhost:8521",
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "calls-1",
      // Apart from other runs' results, which a run clears from its own.
      outputDir: "test-results/calls-b-1",
      use: { ...devices["Desktop Chrome"], launchOptions: { args: fakeMedia } },
      testMatch: /calls-milestone-1\.spec\.ts/,
    },
    /*
     * Presenting a browser tab needs a real tab picker, which the headless
     * shell lacks: the full Chromium, headless, chooses the tab whose title is
     * "Agenda" by itself. Without the fake prompt — which would hand over a
     * fake screen instead of a tab — camera and microphone are granted per
     * context.
     */
    {
      name: "calls-5",
      // Apart from other runs' results, which a run clears from its own.
      outputDir: "test-results/calls-b-5",
      use: {
        ...devices["Desktop Chrome"],
        channel: "chromium",
        launchOptions: {
          args: [
            "--use-fake-device-for-media-stream",
            "--auto-select-tab-capture-source-by-title=Agenda",
          ],
        },
      },
      testMatch: /calls-milestone-5\.spec\.ts/,
    },
    {
      name: "calls-6",
      // Apart from other runs' results, which a run clears from its own.
      outputDir: "test-results/calls-b-6",
      use: { ...devices["Desktop Chrome"], launchOptions: { args: fakeMedia } },
      testMatch: /calls-milestone-6\.spec\.ts/,
    },
  ],
});
