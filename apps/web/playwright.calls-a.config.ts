import { tmpdir } from "node:os";
import { join } from "node:path";

import { defineConfig, devices } from "@playwright/test";

/*
 * Calls milestones 2, 3 and 4, demonstrated in real browsers.
 *
 * Run by scripts/demo-milestone-calls-{2,3,4}.sh, each against servers it
 * starts on databases of its own; each script names its spec. Chromium with a
 * fake camera and microphone, and the permission prompts answered, so a call
 * carries real media without anybody at the machine.
 */
export default defineConfig({
  testDir: "./milestones",
  testMatch: /calls-milestone-[234]\.spec\.ts/,
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  timeout: 300_000,
  // Not test-results/: other suites run beside these and clear that on start.
  outputDir: join(tmpdir(), "tdc-calls-milestones"),
  // A call takes a few seconds to join on a machine busy with other calls.
  expect: { timeout: 20_000 },
  use: {
    baseURL: process.env.TDC_TEST_URL ?? "http://localhost:8511",
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "calls-a",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: {
          args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"],
        },
      },
    },
  ],
});
