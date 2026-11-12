import { execFileSync } from "node:child_process";

import { expect, test, type Page } from "@playwright/test";

import { PASSWORD, signIn, tokenFor } from "./helpers";

/*
 * Calls Milestone 7, first half: a call on a connection that becomes too
 * slow for video steps down to sound, says so, and comes back when the
 * connection does.
 *
 * Run by scripts/demo-milestone-calls-7.sh inside a network namespace of its
 * own, where this process is root and both browsers' media crosses loopback.
 * The throttle is the kernel's (tc), on UDP only — which is how WebRTC media
 * travels here — so the page, the API and the signalling are not slowed.
 * Browser network emulation would not do: it does not touch WebRTC.
 */

const run = Date.now().toString(36);

/** Limits loopback UDP to a rate, or lifts the limit. */
function throttle(rate: string | null) {
  const tc = (...args: string[]) => execFileSync("tc", args, { stdio: "pipe" });
  try {
    tc("qdisc", "del", "dev", "lo", "root");
  } catch {
    // Nothing there yet.
  }
  if (!rate) return;
  tc("qdisc", "add", "dev", "lo", "root", "handle", "1:", "prio");
  tc("qdisc", "add", "dev", "lo", "parent", "1:3", "handle", "30:", "netem", "rate", rate);
  tc(
    ...["filter", "add", "dev", "lo", "parent", "1:", "protocol", "ip", "prio", "1"],
    ...["u32", "match", "ip", "protocol", "17", "0xff", "flowid", "1:3"],
  );
}

const notice = (page: Page) => page.locator("[data-call-window]");

/** Whether the other person's camera is arriving: a decoded remote frame. */
async function seesTheOther(page: Page) {
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const video = document.querySelector<HTMLVideoElement>(
            "[data-call-window] video[data-remote-video]",
          );
          return (video?.readyState ?? 0) >= 2 && (video?.videoWidth ?? 0) > 0;
        }),
      { timeout: 30_000, message: "the other person's camera never arrived" },
    )
    .toBe(true);
}

test.afterAll(() => throttle(null));

test("a call steps down to sound on a slow connection, and recovers", async ({ browser }) => {
  test.setTimeout(420_000);
  const founderContext = await browser.newContext();
  const founder = await founderContext.newPage();
  await signIn(founder, "founder");
  const token = await tokenFor(founder, "founder");
  const auth = { Authorization: `Bearer ${token}` };

  expect(
    (
      await founder.request.put("/api/admin/features/calls", {
        headers: auth,
        data: { enabled: true },
      })
    ).status(),
  ).toBe(204);
  const created = await founder.request.post("/api/rooms", {
    headers: auth,
    data: { slug: `slow-${run}`, name: `slow-${run}` },
  });
  const roomId = ((await created.json()) as { id: string }).id;

  const register = await founder.request.post("/api/auth/register", {
    data: { username: "neema", password: PASSWORD },
  });
  expect(register.status(), "registering the other person").toBe(201);

  await founder.goto(`/rooms/${roomId}`);
  await founder.getByRole("button", { name: "Start a call" }).click();
  await notice(founder).getByRole("button", { name: "Turn camera on" }).click();

  const otherContext = await browser.newContext();
  const other = await otherContext.newPage();
  await signIn(other, "neema");
  const joined = await other.evaluate(
    (path) => fetch(path, { method: "POST", credentials: "include" }).then((r) => r.status),
    `/api/rooms/${roomId}/join`,
  );
  expect(joined).toBeLessThan(300);
  await other.goto(`/rooms/${roomId}`);
  await other
    .locator("[data-call-banner]")
    .getByRole("button", { name: "Join with video" })
    .click();

  await seesTheOther(founder);
  await seesTheOther(other);

  // Too slow for any camera: the call goes on, and the founder is told why
  // their video stopped.
  throttle("100kbit");
  await expect(notice(founder)).toContainText("too slow for your camera", { timeout: 120_000 });
  await expect(notice(founder).getByRole("button", { name: "Leave", exact: true })).toBeVisible();

  // Freed: the camera comes back without anybody doing anything.
  throttle(null);
  await expect(notice(founder)).toContainText("has recovered", { timeout: 180_000 });
  await seesTheOther(other);

  await other.context().close();
  await founderContext.close();
});
