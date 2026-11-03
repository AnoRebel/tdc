import { expect, test, type Browser, type Page } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * Events Milestone 3: a host starts a talk's call from its page; two
 * attendees join from theirs; a guest joins through a link.
 *
 * Chromium runs with a fake camera and microphone (playwright.events.config.ts),
 * so each browser sends real media. Every wait is on what a page shows: a
 * button appearing, a remote video with a decoded frame in it, a name on a
 * tile.
 *
 * Made by scripts/demo-milestone-events-3.sh: neema hosts the talk (and holds
 * call.link through a role); kofi and lulu are in #talks and have not yet
 * answered; calls and guests are on.
 */

const TITLE = "Generics in practice";

function callWindow(page: Page) {
  return page.locator("[data-call-window]");
}

function callSection(page: Page) {
  return page.getByRole("region", { name: "The event's call" });
}

/** Whether another person's camera is arriving here, as tests/calls.spec.ts asks. */
async function seesSomebody(page: Page) {
  await expect
    .poll(
      () =>
        page.evaluate(() =>
          [
            ...document.querySelectorAll<HTMLVideoElement>(
              "[data-call-window] video[data-remote-video]",
            ),
          ].some((video) => {
            const stream = video.srcObject as MediaStream | null;
            const live = stream?.getVideoTracks().some((track) => track.readyState === "live");
            return Boolean(live) && video.readyState >= 2 && video.videoWidth > 0;
          }),
        ),
      { timeout: 30_000, message: "no other person's media ever played" },
    )
    .toBe(true);
}

async function personIn(browser: Browser, baseURL: string, username?: string) {
  const context = await browser.newContext({ baseURL, timezoneId: "Africa/Dar_es_Salaam" });
  const page = await context.newPage();
  if (username) await signIn(page, username);
  return page;
}

test("a talk's call: started by its host, joined by attendees, and by a guest through a link", async ({
  browser,
  baseURL,
}) => {
  const eventId = process.env.TDC_EVENT_ID!;
  expect(eventId, "the script names the talk").toBeTruthy();
  const neema = await personIn(browser, baseURL!, "neema");
  const kofi = await personIn(browser, baseURL!, "kofi");
  const lulu = await personIn(browser, baseURL!, "lulu");

  await test.step("before it starts, an attendee can neither start nor join it", async () => {
    await kofi.goto(`/events/${eventId}`);
    await expect(kofi.getByRole("heading", { name: TITLE })).toBeVisible();
    await expect(callSection(kofi)).toContainText("Answer going or maybe to join the call.");
    await kofi.getByRole("button", { name: "Going", exact: true }).click();
    await expect(callSection(kofi)).toContainText("The call starts when a host starts it.");
    await expect(callSection(kofi).getByRole("button")).toHaveCount(0);
  });

  await test.step("neema, hosting, starts the call from the talk's page", async () => {
    await neema.goto(`/events/${eventId}`);
    await expect(callSection(neema)).toContainText(
      "You are hosting: start the call when it is time.",
    );
    await callSection(neema).getByRole("button", { name: "With video" }).click();
    await expect(
      callWindow(neema).getByRole("button", { name: "Leave", exact: true }),
    ).toBeVisible();
    await expect(callSection(neema)).toContainText("You are in the call");
  });

  await test.step("kofi and lulu join from theirs, and the media flows", async () => {
    // kofi's page was open all along: the call appears on it as it starts.
    await callSection(kofi).getByRole("button", { name: "With video" }).click();
    await expect(
      callWindow(kofi).getByRole("button", { name: "Leave", exact: true }),
    ).toBeVisible();

    await lulu.goto(`/events/${eventId}`);
    await lulu.getByRole("button", { name: "Going", exact: true }).click();
    await expect(callSection(lulu)).toContainText("In the call now: 2");
    await callSection(lulu).getByRole("button", { name: "With video" }).click();
    await expect(
      callWindow(lulu).getByRole("button", { name: "Leave", exact: true }),
    ).toBeVisible();

    for (const page of [neema, kofi, lulu]) await seesSomebody(page);
    await expect(callSection(neema)).toContainText("In the call now: 3");
  });

  let link = "";
  await test.step("neema makes a link that lets guests in, from the call window", async () => {
    await callWindow(neema).getByRole("button", { name: "Links to this call" }).click();
    await callWindow(neema).getByLabel("Label (optional)").fill("Guest speaker");
    // The switch's input is visually hidden; its track is what is pressed.
    const guests = callWindow(neema).getByRole("switch", {
      name: "Let people without an account join",
    });
    await guests.locator("..").click();
    await expect(guests).toBeChecked();
    await callWindow(neema).getByRole("button", { name: "Make link" }).click();
    const made = callWindow(neema).getByRole("status").filter({ hasText: "shown only once" });
    await expect(made.locator("code")).toContainText("/call/");
    link = (await made.locator("code").textContent())!.trim();
  });

  await test.step("grace, who has no account, joins through it as a guest", async () => {
    const grace = await personIn(browser, baseURL!);
    await grace.goto(new URL(link).pathname);
    await expect(grace.getByRole("heading", { name: TITLE })).toBeVisible();
    await grace.getByLabel("Your name, as people in the call will see it").fill("Grace");
    await grace.getByRole("button", { name: "Join as a guest" }).click();
    // A link lets people in through a lobby unless its maker said otherwise:
    // she waits, and the host lets her in from the call window.
    await expect(grace.getByText("Waiting for somebody in the call to let you in")).toBeVisible();
    await callWindow(neema)
      .getByRole("button", { name: /^Let Grace.* in$/ })
      .click();
    await expect(
      callWindow(grace).getByRole("button", { name: "Leave", exact: true }),
    ).toBeVisible();

    // Everybody in the call sees her arrive; she hears them.
    await expect(callWindow(neema).locator("li", { hasText: "Grace" })).toBeVisible();
    await expect(callSection(neema)).toContainText("In the call now: 4");
    await seesSomebody(grace);
    await callWindow(grace).getByRole("button", { name: "Leave", exact: true }).click();
    await expect(callWindow(grace)).toHaveCount(0);
    await grace.context().close();
  });

  for (const page of [lulu, kofi, neema]) {
    await callWindow(page).getByRole("button", { name: "Leave", exact: true }).click();
    await expect(callWindow(page)).toHaveCount(0);
    await page.context().close();
  }
});
