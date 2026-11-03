import {
  expect,
  request as playwrightRequest,
  test,
  type APIRequestContext,
  type Browser,
  type Page,
} from "@playwright/test";

import { choose, people, signIn } from "./helpers";

/*
 * Events Milestone 2: a host invites a channel to a twenty-place workshop; the
 * twenty-first person is waitlisted and moves up when somebody declines;
 * everybody going is reminded a day before.
 *
 * Made by scripts/demo-milestone-events-2.sh: hana (the host), baraka and zuri
 * are people with browsers; p01…p19 answer through the API, as twenty more
 * browsers would add nothing but time. All of them are in #workshops.
 *
 * The workshop starts a day and three minutes from now, so the day-before
 * reminder — the server's default lead of 1440 minutes, sent by a pass every
 * minute — falls due within the demonstration. What is waited on is the
 * reminder arriving on the page, never the clock.
 */

const TITLE = "Intro to Go workshop";
const ZONE = "Africa/Dar_es_Salaam";

/** A day and three minutes from now, on the minute, as Dar es Salaam's wall clock. */
function startInDar(): { start: string; end: string } {
  const minute = 60_000;
  const at = Math.ceil((Date.now() + 24 * 60 * minute + 3 * minute) / minute) * minute;
  // Dar es Salaam is UTC+3 all year.
  const local = (ms: number) => new Date(ms + 3 * 60 * minute).toISOString().slice(0, 16);
  return { start: local(at), end: local(at + 2 * 60 * minute) };
}

async function personIn(browser: Browser, baseURL: string, username: string) {
  const context = await browser.newContext({ baseURL, timezoneId: ZONE });
  const page = await context.newPage();
  await signIn(page, username);
  return page;
}

async function answer(api: APIRequestContext, token: string, eventId: string, status: string) {
  const response = await api.put(`/api/events/${eventId}/rsvp`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { status },
  });
  expect(response.ok(), `answering ${status}: ${await response.text()}`).toBeTruthy();
  return (await response.json()) as { status: string; position?: number };
}

async function notifications(api: APIRequestContext, token: string) {
  // Ungrouped: the list a client shows groups by conversation, and every one
  // of these is in the workshop's.
  const response = await api.get("/api/me/notifications?grouped=false&limit=100", {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as { reason: string; roomId: string }[];
}

function answerButton(page: Page, name: string) {
  return page
    .getByRole("group", { name: "Are you going?" })
    .getByRole("button", { name, exact: true });
}

test("a twenty-place workshop: invited by channel, waitlisted, moved up, reminded", async ({
  browser,
  baseURL,
}) => {
  const everybody = people();
  const api = await playwrightRequest.newContext({ baseURL });
  const attendees = Object.keys(everybody)
    .filter((name) => /^p\d\d$/.test(name))
    .sort();
  expect(attendees).toHaveLength(19);

  // The two people whose turn matters are signed in first, so nothing slow
  // happens between scheduling the workshop and its reminder falling due.
  const baraka = await personIn(browser, baseURL!, "baraka");
  const zuri = await personIn(browser, baseURL!, "zuri");
  const hana = await personIn(browser, baseURL!, "hana");
  let eventId = "";

  await test.step("hana schedules a twenty-place, invite-only workshop in #workshops", async () => {
    const { start, end } = startInDar();
    await hana.goto("/events");
    await hana.getByRole("button", { name: "New event" }).first().click();
    const form = hana.getByRole("dialog", { name: "Schedule an event" });
    await form.getByLabel("Title").fill(TITLE);
    await choose(hana, "Kind", "Workshop");
    await form.getByLabel(/^Starts/).fill(start);
    await form.getByLabel(/^Ends/).fill(end);
    await expect(form.getByLabel("Time zone")).toHaveValue(ZONE);
    await form.getByRole("textbox", { name: "Place", exact: true }).fill("Buni Hub, Dar es Salaam");
    await choose(hana, "Belongs to", "#workshops");
    await choose(hana, "Who can see it", "Invited people only");
    await form.getByRole("spinbutton", { name: "Places" }).fill("20");
    await form.getByRole("button", { name: "Schedule" }).click();
    await hana.waitForURL(/\/events\/[0-9a-f-]{36}$/);
    eventId = hana.url().split("/").pop()!;
    await expect(hana.getByRole("heading", { name: TITLE })).toBeVisible();
    await expect(hana.getByText("20 of 20 places left")).toBeVisible();
  });

  await test.step("before the invitation, #workshops cannot see it", async () => {
    await zuri.goto(`/events/${eventId}`);
    await expect(zuri.getByText("This event does not exist, or you cannot see it.")).toBeVisible();
  });

  await test.step("hana invites the whole of #workshops from the event's page", async () => {
    const who = hana.getByLabel("People or channels");
    await who.fill("workshops");
    await hana.getByRole("button", { name: /workshops\s*Whole channel/ }).click();
    await hana.getByRole("button", { name: "Invite", exact: true }).click();
    await expect(hana.getByText(/Invitations sent: \d+/)).toBeVisible();
    await expect(
      hana.getByRole("button", { name: "Withdraw the invitation to workshops" }),
    ).toBeVisible();
  });

  await test.step("nineteen people answer going; baraka takes the twentieth place", async () => {
    for (const name of attendees) {
      expect((await answer(api, everybody[name]!.token, eventId, "going")).status).toBe("going");
    }
    await baraka.goto(`/events/${eventId}`);
    await expect(baraka.getByRole("heading", { name: TITLE })).toBeVisible();
    await expect(baraka.getByText("1 of 20 places left")).toBeVisible();
    await answerButton(baraka, "Going").click();
    await expect(answerButton(baraka, "Going")).toHaveAttribute("aria-pressed", "true");
    await expect(baraka.getByText("Full — waiting list: 0")).toBeVisible();
  });

  await test.step("zuri, the twenty-first, is put on the waiting list", async () => {
    await zuri.goto(`/events/${eventId}`);
    await expect(zuri.getByRole("heading", { name: TITLE })).toBeVisible();
    await expect(zuri.getByText("Full — waiting list: 0")).toBeVisible();
    await answerButton(zuri, "Going").click();
    await expect(
      zuri.getByText(
        "The event is full. You are number 1 on the waiting list, and will be told if a place frees.",
      ),
    ).toBeVisible();
    await expect(zuri.getByText("Full — waiting list: 1")).toBeVisible();
  });

  await test.step("p07 declines, and zuri moves up and is told", async () => {
    expect((await answer(api, everybody.p07!.token, eventId, "declined")).status).toBe("declined");
    await zuri.reload();
    await expect(answerButton(zuri, "Going")).toHaveAttribute("aria-pressed", "true");
    await expect(zuri.getByText(/You are number \d+ on the waiting list/)).toHaveCount(0);
    await expect(zuri.getByText("Full — waiting list: 0")).toBeVisible();

    await zuri.getByRole("button", { name: /^(Notifications|\d+ unread)/ }).click();
    await expect(
      // Her invitation and this share the workshop's conversation: one entry,
      // saying the newest.
      zuri.getByText(`A place freed up, so you are going to ${TITLE} — 2 new`),
    ).toBeVisible();
    await zuri.keyboard.press("Escape");
  });

  await test.step("a day before it starts, everybody going is reminded — zuri on the page", async () => {
    // zuri keeps the event open, as somebody with TDC open would; the reminder
    // arrives live at the corner of the page.
    await zuri.goto(`/events/${eventId}`);
    const toast = zuri.getByRole("status").filter({ hasText: TITLE });
    await expect(toast.getByText("Starts tomorrow")).toBeVisible({ timeout: 360_000 });

    // And it stays in her notifications.
    await zuri.reload();
    await zuri.getByRole("button", { name: /^(Notifications|\d+ unread)/ }).click();
    await expect(zuri.getByText(`Starting soon: ${TITLE} — 3 new`)).toBeVisible();
  });

  await test.step("…and in the feed every other session reads, the desktop's included", async () => {
    // The desktop application reads the same notifications and live frames as
    // the web client, through a session of its own. zuri's API session here is
    // a second session, signed in by the script, not the browser's.
    const remindedOf = async (name: string) =>
      (await notifications(api, everybody[name]!.token)).filter(
        (each) => each.reason === "event_reminder",
      ).length;
    expect(await remindedOf("zuri"), "zuri, in a second session").toBe(1);
    for (const name of ["hana", "baraka", ...attendees.filter((each) => each !== "p07")]) {
      expect(await remindedOf(name), `${name} is going and is reminded once`).toBe(1);
    }
    expect(await remindedOf("p07"), "p07 declined and is not reminded").toBe(0);
  });

  await api.dispose();
  for (const page of [baraka, zuri, hana]) await page.context().close();
});
