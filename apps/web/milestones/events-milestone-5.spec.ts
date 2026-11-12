import { copyFileSync, utimesSync } from "node:fs";
import { join } from "node:path";

import { expect, test, type Page } from "@playwright/test";

import { choose, signIn } from "./helpers";

/*
 * Events Milestone 5: an exported Google Calendar file imports with its weekly
 * event at the right times, and a subscribed community calendar's new event
 * appears after a refresh.
 *
 * The file is fixtures/google-calendar-export.ics, shaped as Google Calendar
 * exports one: its own VTIMEZONEs, a weekly study group in Dar es Salaam with a
 * week skipped, a call in London time, and an all-day event. The community
 * calendar is fixtures/community-calendar.ics, served by the script from a
 * plain HTTP server on this machine (TDC_COMMUNITY_DIR) — which the server
 * fetches only because the script turned integrations.allow_private_addresses
 * on, as the hosting guide says a test setup must.
 *
 * amina is in Dar es Salaam, in #python, and holds event.calendars.manage
 * through a role.
 */

const FIXTURES = join(import.meta.dirname, "fixtures");

function card(page: Page, title: string, day: string) {
  return page.getByRole("link", { name: `${title}, ${day}`, exact: true });
}

test("a Google Calendar export imports at the right times; a followed calendar refreshes", async ({
  browser,
  baseURL,
}) => {
  const context = await browser.newContext({ baseURL, timezoneId: "Africa/Dar_es_Salaam" });
  const amina = await context.newPage();
  await signIn(amina, "amina");
  await amina.goto("/events");

  await test.step("the export is previewed: three new events, each in its own zone", async () => {
    await amina.getByRole("button", { name: "Import", exact: true }).click();
    const dialog = amina.getByRole("dialog", { name: "Import" });
    await dialog
      .locator('input[type="file"]')
      .setInputFiles(join(FIXTURES, "google-calendar-export.ics"));
    const plans = dialog.getByRole("listitem");
    await expect(plans).toHaveCount(3);
    const study = plans.filter({ hasText: "Python study group" });
    await expect(study).toContainText("New");
    await expect(study).toContainText("Tue 5 Jan 2027 · 18:00 – 20:00 · Africa/Dar_es_Salaam");
    await expect(study).toContainText("Repeats");
    const call = plans.filter({ hasText: "PyCon Africa planning call" });
    await expect(call).toContainText("22:00 – 23:00 · Europe/London");
    await expect(plans.filter({ hasText: "Community sprint day" })).toContainText("All day");
    // Nothing in it is outside what events here support: no warnings.
    await expect(dialog.locator(".text-warning")).toHaveCount(0);

    await choose(amina, "Belongs to", "#python");
    await choose(amina, "Who can see it", "The channel's members");
    await dialog.getByRole("button", { name: "Import (3)" }).click();
    await expect(dialog.getByText("Created 3, updated 0, skipped 0.")).toBeVisible();
    await dialog.getByRole("button", { name: "Close" }).first().click();
  });

  await test.step("the weekly study group is at 18:00 every Tuesday but the one skipped", async () => {
    await expect(card(amina, "Python study group", "5 Jan")).toContainText("18:00 – 20:00");
    await expect(card(amina, "Python study group", "12 Jan")).toContainText("18:00 – 20:00");
    await expect(card(amina, "Python study group", "26 Jan")).toContainText("18:00 – 20:00");
    // Skipped in Google Calendar, skipped here.
    await expect(card(amina, "Python study group", "19 Jan")).toHaveCount(0);
    // The London call, at Dar es Salaam's time, with London's beside it.
    const call = card(amina, "PyCon Africa planning call", "12 Jan");
    await expect(call).toContainText("22:00 – 23:00");
    await expect(call).toContainText("Tue 19:00 GMT");
    await expect(card(amina, "Community sprint day", "16 Jan")).toContainText("All day");
  });

  await test.step("importing the same file again would create nothing twice", async () => {
    await amina.getByRole("button", { name: "Import", exact: true }).click();
    const dialog = amina.getByRole("dialog", { name: "Import" });
    await dialog
      .locator('input[type="file"]')
      .setInputFiles(join(FIXTURES, "google-calendar-export.ics"));
    await expect(dialog.getByRole("listitem")).toHaveCount(3);
    await expect(dialog.getByRole("listitem").filter({ hasText: "Update" })).toHaveCount(3);
    await dialog.getByRole("button", { name: "Close" }).first().click();
  });

  const community = process.env.TDC_COMMUNITY_URL!;
  await test.step("amina follows the Nairobi Gophers' calendar", async () => {
    await amina.getByRole("button", { name: "Followed calendars" }).click();
    const dialog = amina.getByRole("dialog", { name: "Followed calendars" });
    await dialog.getByLabel("Calendar address").fill(community);
    await dialog.getByRole("textbox", { name: /^Name/ }).fill("Nairobi Gophers");
    await dialog.getByRole("button", { name: "Follow a calendar" }).click();
    await expect(dialog.getByText("Nairobi Gophers", { exact: true })).toBeVisible();
    await expect(dialog.getByText(/^Last fetched /)).toBeVisible();
    await dialog.getByRole("button", { name: "Close" }).first().click();
    await expect(card(amina, "Nairobi Gophers meetup", "14 Jan")).toContainText("18:30 – 20:30");
    await expect(amina.getByRole("link", { name: /^Go 1\.28 release party/ })).toHaveCount(0);
  });

  await test.step("the community adds an event; after a refresh it is here", async () => {
    const served = join(process.env.TDC_COMMUNITY_DIR!, "community.ics");
    copyFileSync(join(FIXTURES, "community-calendar-updated.ics"), served);
    // Its modification time moved on, as a publisher's would: the server asks
    // with If-Modified-Since, and a file changed within the same second would
    // honestly be answered as unchanged.
    const later = new Date(Date.now() + 5_000);
    utimesSync(served, later, later);

    // Not before it is fetched again.
    await amina.reload();
    await expect(card(amina, "Nairobi Gophers meetup", "14 Jan")).toBeVisible();
    await expect(amina.getByRole("link", { name: /^Go 1\.28 release party/ })).toHaveCount(0);

    await amina.getByRole("button", { name: "Followed calendars" }).click();
    const dialog = amina.getByRole("dialog", { name: "Followed calendars" });
    await dialog.getByRole("button", { name: "Refresh now" }).click();
    await expect(dialog.getByRole("button", { name: "Refresh now" })).toBeEnabled();
    await dialog.getByRole("button", { name: "Close" }).first().click();
    const party = card(amina, "Go 1.28 release party", "11 Feb");
    await expect(party).toContainText("18:30 – 21:00");
    await party.click();
    await expect(amina.getByText("From a followed calendar")).toBeVisible();
  });

  await context.close();
});
