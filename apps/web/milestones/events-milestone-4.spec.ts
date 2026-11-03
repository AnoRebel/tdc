import { writeFileSync } from "node:fs";

import { expect, request as playwrightRequest, test } from "@playwright/test";

import { signIn } from "./helpers";

/*
 * Events Milestone 4: a person adds their calendar feed to a calendar
 * application and sees their meetups there at the right local time.
 *
 * james, in London, makes his calendar address in Settings, as the user guide
 * says, and copies what the page shows him. What a calendar application does
 * with it — fetch it with no session, read it, put each occurrence at the
 * right local time — is done by the script with a real iCalendar parser; this
 * spec fetches it the way such an application would, and saves what came back
 * (TDC_FEED_OUT) for the script to read.
 *
 * Made by scripts/demo-milestone-events-4.sh: james is in #dar-devs, which has
 * a weekly meetup at 18:00 Dar es Salaam time; #elsewhere, which he is not in,
 * has an event of its own.
 */

test("james makes his calendar address, and it serves his meetups to anything that asks", async ({
  browser,
  baseURL,
}) => {
  const context = await browser.newContext({ baseURL, timezoneId: "Europe/London" });
  const james = await context.newPage();
  await signIn(james, "james");

  await test.step("in London, the Events page shows the meetup at 15:00", async () => {
    await james.goto("/events");
    const meetup = james.getByRole("link", { name: /^Dar Devs weekly meetup, / }).first();
    await expect(meetup).toContainText("15:00 – 17:00");
    await expect(james.getByRole("link", { name: /^Elsewhere social, / })).toHaveCount(0);
  });

  let address = "";
  await test.step("Settings → Your calendar → Make my calendar address", async () => {
    await james.getByRole("link", { name: "Calendar feed" }).click();
    const section = james.locator("#calendar-feed");
    await expect(section.getByRole("heading", { name: "Your calendar" })).toBeVisible();
    await section.getByRole("button", { name: "Make my calendar address" }).click();
    await expect(
      section.getByText(
        "Copy this address now: it is shown once. Anybody holding it can read your calendar.",
      ),
    ).toBeVisible();
    address = await section.getByLabel("Your calendar address").inputValue();
    expect(address).toMatch(/\/api\/calendar\/feeds\/[^/]+\/events\.ics$/);
    // The same address, offered to a calendar application to open directly.
    await expect(
      section.getByRole("link", { name: "Open in your calendar application" }),
    ).toHaveAttribute("href", address.replace(/^https?:/, "webcal:"));
  });

  await test.step("a calendar application fetches it, with no session at all", async () => {
    const application = await playwrightRequest.newContext();
    const response = await application.get(address);
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toContain("text/calendar");
    const body = await response.text();
    expect(body).toContain("BEGIN:VCALENDAR");
    writeFileSync(process.env.TDC_FEED_OUT!, body);
    await application.dispose();
  });

  await test.step("and the page now says the address exists, not what it is", async () => {
    await james.reload();
    const section = james.locator("#calendar-feed");
    await expect(section.getByText(/You have a calendar address, made /)).toBeVisible();
    await expect(section.getByLabel("Your calendar address")).toHaveCount(0);
  });

  await context.close();
});
