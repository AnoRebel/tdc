import { expect, test, type Browser, type Page } from "@playwright/test";

import { choose, people, signIn } from "./helpers";

/*
 * Events Milestone 1: a member schedules a weekly meetup in a channel in Dar es
 * Salaam time; somebody in London sees it at their own time with the
 * organiser's beside it; somebody outside the channel cannot find it.
 *
 * Each person has a browser of their own in their own time zone, so what each
 * sees is what a person there would: nothing here converts a time for them.
 * Made by scripts/demo-milestone-events-1.sh: amina and james are in
 * #dar-devs, otto is not.
 */

const TITLE = "Dar Devs weekly meetup";

/** The first Thursday at least two days away, as the date a form takes. */
function nextThursday(): string {
  const day = new Date();
  day.setUTCDate(day.getUTCDate() + 2);
  while (day.getUTCDay() !== 4) day.setUTCDate(day.getUTCDate() + 1);
  return day.toISOString().slice(0, 10);
}

async function personIn(browser: Browser, baseURL: string, username: string, timezoneId: string) {
  const context = await browser.newContext({ baseURL, timezoneId });
  const page = await context.newPage();
  await signIn(page, username);
  return page;
}

function card(page: Page, month?: string) {
  return page.getByRole("link", { name: new RegExp(`^${TITLE}, \\d+ ${month ?? ""}`) });
}

test("a weekly meetup in Dar es Salaam time, seen from London, hidden from outsiders", async ({
  browser,
  baseURL,
}) => {
  const everybody = people();
  const thursday = nextThursday();
  let eventId = "";

  await test.step("amina, in Dar es Salaam, schedules a weekly meetup in #dar-devs", async () => {
    const amina = await personIn(browser, baseURL!, "amina", "Africa/Dar_es_Salaam");
    await amina.goto("/events");
    await amina.getByRole("button", { name: "New event" }).first().click();
    const form = amina.getByRole("dialog", { name: "Schedule an event" });
    await expect(form).toBeVisible();

    await form.getByLabel("Title").fill(TITLE);
    await form.getByLabel("Description").fill("Lightning talks and **chai**. Every Thursday.");
    await form.getByLabel(/^Starts/).fill(`${thursday}T18:00`);
    await form.getByLabel(/^Ends/).fill(`${thursday}T20:00`);
    // The form opens in the organiser's own zone, and reads the times in it.
    await expect(form.getByLabel("Time zone")).toHaveValue("Africa/Dar_es_Salaam");

    await choose(amina, "Repeats", "Weekly");
    const thu = form.getByRole("button", { name: "Thu", exact: true });
    if ((await thu.getAttribute("aria-pressed")) !== "true") await thu.click();
    await expect(thu).toHaveAttribute("aria-pressed", "true");

    await form
      .getByRole("textbox", { name: "Place", exact: true })
      .fill("Kijitonyama, Dar es Salaam");
    await choose(amina, "Belongs to", "#dar-devs");
    await choose(amina, "Who can see it", "The channel's members");
    await form.getByRole("button", { name: "Schedule" }).click();

    await amina.waitForURL(/\/events\/[0-9a-f-]{36}$/);
    eventId = amina.url().split("/").pop()!;
    await expect(amina.getByRole("heading", { name: TITLE })).toBeVisible();
    // The organiser sees her own time, and no second line: it is hers.
    await expect(amina.getByText("18:00 – 20:00")).toBeVisible();
    await expect(amina.getByText(/for the organiser/)).toHaveCount(0);
    await expect(amina.getByText("Weekly", { exact: true })).toBeVisible();
    await amina.context().close();
  });

  await test.step("james, in London, sees it at his own time with hers beside it", async () => {
    const james = await personIn(browser, baseURL!, "james", "Europe/London");
    await james.goto("/events");
    // Every occurrence is its own entry: a weekly series fills the agenda.
    await expect(card(james).first()).toBeVisible();
    expect(await card(james).count()).toBeGreaterThanOrEqual(8);

    // November: London is on GMT, three hours behind Dar es Salaam.
    const first = card(james).first();
    await expect(first).toContainText("15:00 – 17:00");
    await expect(first).toContainText(/Thu 18:00 (EAT|GMT\+3)/);

    // April: London has moved to summer time and Dar es Salaam has not. The
    // meetup stays at 18:00 for its organiser, so it moves for London.
    const april = card(james, "Apr").first();
    await april.scrollIntoViewIfNeeded();
    await expect(april).toContainText("16:00 – 18:00");
    await expect(april).toContainText(/Thu 18:00 (EAT|GMT\+3)/);

    await first.click();
    await james.waitForURL(`**/events/${eventId}`);
    await expect(james.getByRole("heading", { name: TITLE })).toBeVisible();
    await expect(james.getByText("15:00 – 17:00")).toBeVisible();
    await expect(james.getByText(/Thu 18:00 (EAT|GMT\+3) for the organiser/)).toBeVisible();
    await james.context().close();
  });

  await test.step("otto, outside the channel, cannot find it", async () => {
    const otto = await personIn(browser, baseURL!, "otto", "Africa/Nairobi");
    await otto.goto("/events");
    await expect(otto.getByText("Nothing coming up")).toBeVisible();
    await expect(card(otto)).toHaveCount(0);

    // Its own address answers as if there were no such event.
    await otto.goto(`/events/${eventId}`);
    await expect(otto.getByText("This event does not exist, or you cannot see it.")).toBeVisible();
    await expect(otto.getByRole("heading", { name: TITLE })).toHaveCount(0);

    // And so does the API, word for word as for an event that never existed.
    const headers = { Authorization: `Bearer ${everybody.otto!.token}` };
    const request = otto.request;
    const hidden = await request.get(`/api/events/${eventId}`, { headers });
    const missing = await request.get(`/api/events/00000000-0000-4000-8000-000000000000`, {
      headers,
    });
    expect(hidden.status()).toBe(404);
    expect(missing.status()).toBe(404);
    expect(await hidden.text()).toBe(await missing.text());

    const listed = (await (await request.get("/api/events", { headers })).json()) as {
      title: string;
    }[];
    expect(listed.map((each) => each.title)).not.toContain(TITLE);

    const searched = (await (
      await request.get(`/api/search/everything?q=${encodeURIComponent("weekly meetup")}`, {
        headers,
      })
    ).json()) as { messages: unknown[]; rooms: unknown[] };
    expect(searched.rooms).toEqual([]);
    expect(searched.messages).toEqual([]);
    for (const path of [`/api/events/${eventId}/ics`, `/api/events/${eventId}/attendees`]) {
      expect((await request.get(path, { headers })).status(), path).toBe(404);
    }
    await otto.context().close();
  });
});
