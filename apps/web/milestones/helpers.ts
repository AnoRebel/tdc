import { readFileSync } from "node:fs";

import { expect, type Page } from "@playwright/test";

export const PASSWORD = "correct horse battery staple";

/**
 * Signs in through the sign-in page, as a person would, and dismisses the
 * introduction if this account has not seen it yet.
 */
export async function signIn(page: Page, username: string): Promise<void> {
  await page.goto("/signin");
  await page.getByLabel("Username or email").fill(username);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));

  // Waited for rather than asked about: the tour renders a moment after the
  // page, and isVisible() answers before it has.
  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 5000 }).catch(() => {});
  if (await tour.count()) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }
}

/** Opens one of a project's views by its tab. */
export async function openTab(page: Page, name: string): Promise<void> {
  await page
    .getByRole("navigation", { name: "Ways of looking at this project" })
    .getByRole("link", { name, exact: true })
    .click();
}

/** Chooses an option from one of the shared Select controls. */
export async function choose(page: Page, label: string, option: string): Promise<void> {
  await page.getByRole("combobox", { name: label }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
  // Closed before anything else is touched: while the list is open it holds
  // the pointer, and a click meant for the page lands on its backdrop.
  await expect(page.getByRole("listbox")).toHaveCount(0);
}

/** A bearer token for an account, for fixtures made through the API. */
export async function tokenFor(page: Page, username: string): Promise<string> {
  const response = await page.request.post("/api/auth/signin", {
    data: { username, password: PASSWORD },
  });
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { token: string }).token;
}

/** Somebody a demonstration script made, with their id and a bearer token. */
export interface Person {
  id: string;
  token: string;
}

/** The people the script made for this spec (TDC_PEOPLE_FILE), by username. */
export function people(): Record<string, Person> {
  const file = process.env.TDC_PEOPLE_FILE;
  if (!file) throw new Error("TDC_PEOPLE_FILE names the people the script made");
  return JSON.parse(readFileSync(file, "utf8")) as Record<string, Person>;
}

/**
 * Registers somebody and gives a bearer token for them. Registration is
 * limited to five an hour from one address, so a demonstration registers only
 * the people it needs.
 */
export async function registered(page: Page, username: string): Promise<string> {
  const response = await page.request.post("/api/auth/register", {
    data: { username, password: PASSWORD },
  });
  expect(response.status(), `registering ${username}: ${await response.text()}`).toBe(201);
  return tokenFor(page, username);
}

/** Waits until the one other person's camera arrives in this page's call. */
export async function seesTheOther(page: Page): Promise<void> {
  await seesCameras(page, 1, 45_000);
}

/**
 * Every call a page was rung into, by call id, as the live connection
 * delivered it: listened to from before the page opens its connection, so a
 * ring that arrives and is gone again is still counted.
 */
export function ringsArriving(page: Page): Set<string> {
  const calls = new Set<string>();
  page.on("websocket", (socket) => {
    socket.on("framereceived", ({ payload }) => {
      for (const line of String(payload).split("\n")) {
        try {
          const frame = JSON.parse(line) as { type?: string; payload?: { call?: string } };
          if (frame.type === "call.ring" && frame.payload?.call) calls.add(frame.payload.call);
        } catch {
          // Not a frame of ours.
        }
      }
    });
  });
  return calls;
}

/*
 * Calls.
 */

/** An API call from inside a signed-in page, on that page's session. */
export async function inPage(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string }> {
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

/** The call window, wherever it sits on the page. */
export function callWindow(page: Page) {
  return page.locator("[data-call-window]");
}

/**
 * Keeps every RTCPeerConnection a page makes in `window.__peers`, so a
 * demonstration can ask the connections themselves how media travels. Added
 * before the page loads.
 */
export async function watchPeerConnections(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const Original = window.RTCPeerConnection;
    const peers: RTCPeerConnection[] = [];
    (window as unknown as { __peers: RTCPeerConnection[] }).__peers = peers;
    window.RTCPeerConnection = class extends Original {
      constructor(...args: ConstructorParameters<typeof RTCPeerConnection>) {
        super(...args);
        peers.push(this);
      }
    };
  });
}

/** One connected peer connection's chosen route: the two candidates' kinds and addresses. */
export interface ChosenRoute {
  local: string;
  remote: string;
  localAddress: string;
  remoteAddress: string;
}

/** The route each connected peer connection on a page is using. */
export async function chosenRoutes(page: Page): Promise<ChosenRoute[]> {
  return page.evaluate(async () => {
    const peers = (window as unknown as { __peers?: RTCPeerConnection[] }).__peers ?? [];
    const routes: ChosenRoute[] = [];
    for (const peer of peers) {
      if (peer.connectionState !== "connected") continue;
      const stats = new Map<string, Record<string, unknown>>();
      (await peer.getStats()).forEach((each: Record<string, unknown>) =>
        stats.set(each.id as string, each),
      );
      const transport = [...stats.values()].find((each) => each.type === "transport");
      const pair = stats.get(transport?.selectedCandidatePairId as string);
      if (!pair) continue;
      const local = stats.get(pair.localCandidateId as string) ?? {};
      const remote = stats.get(pair.remoteCandidateId as string) ?? {};
      routes.push({
        local: String(local.candidateType),
        remote: String(remote.candidateType),
        localAddress: `${local.address}:${local.port}`,
        remoteAddress: `${remote.address}:${remote.port}`,
      });
    }
    return routes;
  });
}

/**
 * How many other people's cameras are arriving in the call window: remote
 * videos with a live track and a decoded frame, which only media that
 * actually crossed a connection gives.
 */
export async function camerasArriving(page: Page): Promise<number> {
  return page.evaluate(
    () =>
      [
        ...document.querySelectorAll<HTMLVideoElement>(
          "[data-call-window] video[data-remote-video]",
        ),
      ].filter((video) => {
        const stream = video.srcObject as MediaStream | null;
        const live = stream?.getVideoTracks().some((track) => track.readyState === "live");
        return Boolean(live) && video.readyState >= 2 && video.videoWidth > 0;
      }).length,
  );
}

/** Waits until a page receives the cameras of this many other people. */
export async function seesCameras(page: Page, count: number, timeout = 60_000): Promise<void> {
  await expect
    .poll(() => camerasArriving(page), {
      timeout,
      message: `expected ${count} other people's cameras to arrive`,
    })
    .toBe(count);
}

/**
 * Records the introduction as seen for the signed-in person, so it cannot
 * appear over the page later: on a loaded machine it can render after
 * signIn() has stopped waiting for it. Takes effect from the next page load.
 */
export async function skipTours(page: Page): Promise<void> {
  const answer = await inPage(page, "PUT", "/api/me/preferences/tours", {
    seen: ["chat"],
    reached: {},
  });
  expect(answer.status, `recording the tour as seen: ${answer.text}`).toBeLessThan(300);
}

/**
 * Marks the introduction seen for somebody, before they sign in. On a machine
 * busy enough, the tour can open after signIn has stopped waiting for it, and
 * its overlay then takes the next click.
 */
export async function tourSeen(page: Page, token: string): Promise<void> {
  const response = await page.request.put("/api/me/preferences/tours", {
    headers: { Authorization: `Bearer ${token}` },
    data: { seen: ["chat"], reached: {} },
  });
  expect(response.ok(), `marking the tour seen: ${await response.text()}`).toBeTruthy();
}

/**
 * How many other people's voices are arriving on a page: incoming audio
 * streams, on every connected peer connection, that have carried at least a
 * second of sound. Sound is never stepped down, so on a machine too loaded to
 * keep every camera going this still says whether each person's media
 * crosses to each other person.
 */
export async function voicesArriving(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const peers = (window as unknown as { __peers?: RTCPeerConnection[] }).__peers ?? [];
    let voices = 0;
    for (const peer of peers) {
      if (peer.connectionState !== "connected") continue;
      (await peer.getStats()).forEach((each: Record<string, unknown>) => {
        if (
          each.type === "inbound-rtp" &&
          each.kind === "audio" &&
          Number(each.packetsReceived) > 50
        ) {
          voices += 1;
        }
      });
    }
    return voices;
  });
}

/** Waits until a page hears this many other people. */
export async function hearsVoices(page: Page, count: number, timeout = 60_000): Promise<void> {
  await expect
    .poll(() => voicesArriving(page), {
      timeout,
      message: `expected ${count} other people's voices to arrive`,
    })
    .toBe(count);
}
