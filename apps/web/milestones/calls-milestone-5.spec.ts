import { expect, test, type Browser, type Page } from "@playwright/test";

import { callWindow, inPage, signIn, skipTours } from "./helpers";

/*
 * Calls Milestone 5: one person presents a browser tab, another records with
 * everybody told, the recording appears in the conversation, and the
 * conversation and the audit log show who recorded and when.
 *
 * Run by scripts/demo-milestone-calls-5.sh, which has turned calls and
 * recording on and given juma a role holding call.record, as an operator
 * would. The browser is the full Chromium, headless, which shares the tab
 * titled "Agenda" when asked to share something.
 */

const run = Date.now().toString(36);

async function person(browser: Browser, username: string, baseURL: string): Promise<Page> {
  const context = await browser.newContext({ baseURL, permissions: ["camera", "microphone"] });
  const page = await context.newPage();
  await signIn(page, username);
  await skipTours(page);
  return page;
}

test("a tab presented, a call recorded with everybody told, and the record of it", async ({
  browser,
  baseURL,
}) => {
  const founder = await person(browser, "founder", baseURL!);
  const neema = await person(browser, "neema", baseURL!);
  const juma = await person(browser, "juma", baseURL!);

  const created = await inPage(founder, "POST", "/api/rooms", {
    slug: `review-${run}`,
    name: `review-${run}`,
  });
  expect(created.status, `making the channel: ${created.text}`).toBeLessThan(300);
  const roomId = (JSON.parse(created.text) as { id: string }).id;
  for (const page of [neema, juma]) {
    expect((await inPage(page, "POST", `/api/rooms/${roomId}/join`)).status).toBeLessThan(300);
  }

  // The tab neema will present, open beside TDC.
  const agenda = await neema.context().newPage();
  await agenda.setContent(
    "<title>Agenda</title><body style='font:48px sans-serif;background:#fde68a'>" +
      "<h1>Quarterly review</h1><ol><li>Numbers</li><li>Plans</li></ol></body>",
  );
  // What neema's browser hands over when she shares: kept to show it was a tab.
  await neema.addInitScript(() => {
    const media = navigator.mediaDevices;
    const original = media.getDisplayMedia.bind(media);
    media.getDisplayMedia = async (options) => {
      const stream = await original(options);
      (window as unknown as { __shared: string }).__shared = String(
        stream.getVideoTracks()[0]?.getSettings().displaySurface,
      );
      return stream;
    };
  });

  await neema.goto(`/rooms/${roomId}`);
  await neema.bringToFront();
  await neema.getByRole("button", { name: "Start a call" }).click();
  await juma.goto(`/rooms/${roomId}`);
  await juma
    .locator("[data-call-banner]")
    .getByRole("button", { name: "Join", exact: true })
    .click();
  await expect(callWindow(neema).locator("li", { hasText: "juma" })).toBeVisible();

  // neema presents the Agenda tab; it takes the stage on juma's screen.
  await callWindow(neema).getByRole("button", { name: "Share your screen" }).click();
  await expect(callWindow(neema)).toContainText("You are presenting");
  expect(await neema.evaluate(() => (window as unknown as { __shared?: string }).__shared)).toBe(
    "browser",
  );
  await expect(callWindow(juma).locator("[data-call-stage]")).toContainText("neema is presenting");
  const stage = callWindow(juma).locator("[data-call-stage] video");
  await expect
    .poll(() => stage.evaluate((video: HTMLVideoElement) => video.videoWidth), {
      timeout: 30_000,
      message: "the presented tab never arrived",
    })
    .toBeGreaterThan(0);

  // juma records. Everybody is told before his device records anything.
  await callWindow(juma).getByRole("button", { name: "Record the call" }).click();
  await expect(callWindow(neema)).toContainText("Recorded by juma");
  await expect(neema.getByRole("status").filter({ hasText: "juma started recording" })).toHaveCount(
    1,
  );
  await expect(callWindow(juma)).toContainText("You are recording this call");

  // A few seconds of the presentation, measured by the stage itself.
  const from = await stage.evaluate((video: HTMLVideoElement) => video.currentTime);
  await expect
    .poll(() => stage.evaluate((video: HTMLVideoElement) => video.currentTime), {
      timeout: 30_000,
    })
    .toBeGreaterThan(from + 3);

  await callWindow(juma).getByRole("button", { name: "Stop recording" }).click();
  await expect(callWindow(juma)).toContainText("The recording is in the conversation.", {
    timeout: 60_000,
  });
  await expect(callWindow(neema)).not.toContainText("Recorded by juma");

  // The conversation: the recording, and who recorded, for both of them.
  for (const page of [neema, juma]) {
    // A message from juma holding the file, played where it is.
    await expect(page.locator('video[aria-label^="Call recording"]')).toHaveCount(1);
    await expect(
      page.getByRole("note").filter({ hasText: "juma started recording the call" }),
    ).toBeVisible();
    await expect(
      page.getByRole("note").filter({ hasText: /juma stopped recording, after/ }),
    ).toBeVisible();
  }

  // The audit log, in Administration: who, what, and when.
  await founder.goto("/audit");
  for (const action of ["call.recording.started", "call.recording.stopped"]) {
    const row = founder.getByRole("row").filter({ hasText: action });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText("juma");
  }
  const entries = JSON.parse(
    (await inPage(founder, "GET", "/api/admin/audit?action=call.recording.started")).text,
  ) as { actor: string; occurredAt: string }[];
  expect(entries.map((each) => each.actor)).toEqual(["juma"]);
  // eslint-disable-next-line no-console -- what the demonstration saw, for its summary
  console.log(`  audit: juma started recording at ${entries[0]!.occurredAt}`);

  await callWindow(juma).getByRole("button", { name: "Leave", exact: true }).click();
  await callWindow(neema).getByRole("button", { name: "Leave", exact: true }).click();
  for (const page of [founder, neema, juma]) await page.context().close();
});
