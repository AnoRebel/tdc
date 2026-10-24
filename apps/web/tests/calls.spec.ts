import {
  expect,
  request as playwrightRequest,
  test,
  type Browser,
  type Page,
} from "@playwright/test";

import { SESSION } from "../playwright.config";

/*
 * A two-person call, in a real browser with fake camera and microphone.
 *
 * The server's tests prove the signalling; the client's prove the state. What
 * neither shows is the media actually arriving: two browsers negotiating,
 * connecting, and each playing the other. So every wait here is on what the
 * page shows — a remote video with data in it, a mute mark on a tile, a line
 * in the timeline — never on time passing.
 *
 * Run by the calls-* projects, which launch each engine with fake media
 * devices and permission prompts answered.
 */

const password = "correct horse battery staple";
const run = Date.now().toString(36);
// One callee across runs and engines: registration is limited per address, so
// each run signs the same person in rather than registering another.
const callee = "callee";

// eslint-disable-next-line no-empty-pattern -- Playwright passes fixtures first
test.beforeEach(({}, testInfo) => {
  testInfo.skip(
    !testInfo.project.name.startsWith("calls"),
    "run by the calls projects, which give the browser fake media devices",
  );
});

/*
 * An API call from inside a signed-in page, on that page's session.
 *
 * Rather than a sign-in of its own: the founder's eight sign-ins a quarter
 * hour are shared with every other spec in the run, and the stored session is
 * already signed in.
 */
async function inPage(page: Page, method: string, path: string, body?: unknown) {
  return page.evaluate(
    async ({ method, path, body }) => {
      const response = await fetch(path, {
        method,
        credentials: "include",
        headers: body === undefined ? {} : { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: response.status, text: await response.text() };
    },
    { method, path, body },
  );
}

/** The other person, in a browser of their own, signed in through the form. */
async function calleePage(browser: Browser, baseURL: string) {
  const context = await browser.newContext({
    baseURL,
    // Chromium and Firefox answer from their launch flags; WebKit only from
    // granted permissions.
    permissions: test.info().project.name === "calls-webkit" ? ["camera", "microphone"] : [],
  });
  // Registered only when signing in shows there is nobody yet: a registration
  // for a taken name still spends one of the five an hour an address has.
  const outside = await playwrightRequest.newContext({ baseURL });
  const exists = await outside.post("/api/auth/signin", { data: { username: callee, password } });
  if (!exists.ok()) {
    const registered = await outside.post("/api/auth/register", {
      data: { username: callee, password },
    });
    expect(registered.status(), `registering ${callee}`).toBe(201);
  }
  await outside.dispose();
  const page = await context.newPage();
  await page.goto("/signin");
  await page.getByLabel("Username or email").fill(callee);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));
  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 5000 }).catch(() => {});
  if (await tour.count()) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }
  return page;
}

/*
 * Whether the other person's camera is arriving here: a remote video with a
 * decoded frame in it, which only a connection that carries media gives.
 *
 * Video rather than sound because a stream holding a camera that sends
 * nothing waits for its first frame before saying anything about its sound,
 * so a voice-only tile has no readiness to wait on.
 */
async function hearsTheOther(page: Page) {
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const video = document.querySelector<HTMLVideoElement>(
            "[data-call-window] video[data-remote-video]",
          );
          const stream = video?.srcObject as MediaStream | null | undefined;
          const live = stream?.getVideoTracks().some((track) => track.readyState === "live");
          // HAVE_CURRENT_DATA, and a frame with a size: something was decoded.
          return Boolean(live) && (video?.readyState ?? 0) >= 2 && (video?.videoWidth ?? 0) > 0;
        }),
      { timeout: 30_000, message: "the other person's media never played" },
    )
    .toBe(true);
}

function callWindow(page: Page) {
  return page.locator("[data-call-window]");
}

test("two people call, see each other, mute, and leave", async ({ browser, baseURL }) => {
  const founderContext = await browser.newContext({
    baseURL,
    storageState: SESSION,
    permissions: test.info().project.name === "calls-webkit" ? ["camera", "microphone"] : [],
  });
  const founder = await founderContext.newPage();
  await founder.goto("/");
  const toggled = await inPage(founder, "PUT", "/api/admin/features/calls", { enabled: true });
  expect(toggled.status, "turning calls on").toBe(204);
  // A channel of its own, so the call lines it leaves are nobody else's.
  const created = await inPage(founder, "POST", "/api/rooms", {
    slug: `call-${run}`,
    name: `call-${run}`,
  });
  expect(created.status, `making the channel: ${created.text}`).toBeLessThan(300);
  const roomId = (JSON.parse(created.text) as { id: string }).id;
  // Opened afresh, so the application reads that the server now offers calls.
  await founder.goto(`/rooms/${roomId}`);

  // An engine without WebRTC hides calls, and the honest answer is a skip
  // naming why rather than a failure about a missing button.
  const webrtc = await founder.evaluate(
    () => typeof RTCPeerConnection === "function" && Boolean(navigator.mediaDevices?.getUserMedia),
  );
  test.skip(!webrtc, "this engine build has no WebRTC");

  // Start.
  await founder.getByRole("button", { name: "Start a call" }).click();
  await expect(
    callWindow(founder).getByRole("button", { name: "Leave", exact: true }),
  ).toBeVisible();
  await callWindow(founder).getByRole("button", { name: "Turn camera on" }).click();

  // The other joins from the bar the conversation shows, with their camera.
  const other = await calleePage(browser, baseURL!);
  const joined = await inPage(other, "POST", `/api/rooms/${roomId}/join`);
  expect(joined.status, "the callee joining the channel").toBeLessThan(300);
  await other.goto(`/rooms/${roomId}`);
  const banner = other.locator("[data-call-banner]");
  await banner.getByRole("button", { name: "Join with video" }).click();
  await expect(callWindow(other).getByRole("button", { name: "Leave", exact: true })).toBeVisible();

  // Both see the other's camera arrive.
  await hearsTheOther(founder);
  await hearsTheOther(other);

  // Muting shows on the other side, on that person's tile.
  await callWindow(other).getByRole("button", { name: "Mute", exact: true }).click();
  const calleeTile = callWindow(founder).locator("li", { hasText: callee });
  await expect(calleeTile.locator('[aria-label="Muted"]')).toBeVisible();

  // Leaving: the last one out ends the call, and its end is a line in the
  // conversation.
  await callWindow(other).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(callWindow(other)).toHaveCount(0);
  await callWindow(founder).getByRole("button", { name: "Leave", exact: true }).click();
  await expect(callWindow(founder)).toHaveCount(0);
  await expect(founder.getByRole("note").filter({ hasText: "The call ended after" })).toBeVisible();
  await expect(founder.getByRole("note").filter({ hasText: "started a call" })).toBeVisible();

  await other.context().close();
  await founderContext.close();
});
