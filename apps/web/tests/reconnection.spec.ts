import { expect, request as playwrightRequest, test } from "@playwright/test";

/*
 * What a connection that dropped brings back with it.
 *
 * The server's tests prove it serves exactly the gap somebody missed, and the
 * client's own tests prove it asks from the right position. Neither shows the
 * thing that actually goes wrong: a client that reconnects, replays the gap it
 * asked for, and also keeps what the socket delivered before it noticed — so
 * the person reads the same message twice.
 *
 * Duplicates and omissions are the two failures, and they look identical from
 * inside the code that caused them. This counts what is on screen.
 */

const CHANNEL = "General";

async function openChannel(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page
    .getByRole("link", { name: new RegExp(CHANNEL, "i") })
    .first()
    .click();
  await page.getByRole("textbox", { name: /message/i }).waitFor();
}

/** How many times a given message body appears in the timeline. */
async function timesShown(page: import("@playwright/test").Page, body: string) {
  return page.getByText(body, { exact: true }).count();
}

test("a partition loses nothing and repeats nothing", async ({ page, context, baseURL }) => {
  await openChannel(page);

  /*
   * Sent while connected, so there is a known-good message before the
   * partition: a test that only checks what arrives during the outage cannot
   * tell "recovered correctly" from "lost everything and started again".
   */
  const before = `before the partition ${Date.now().toString(36)}`;
  await page.getByRole("textbox", { name: /message/i }).fill(before);
  await page.keyboard.press("Enter");
  await expect(page.getByText(before, { exact: true })).toHaveCount(1);

  /*
   * The partition. setOffline cuts the socket the way a tunnel does — the
   * page stays loaded and the connection stops — which is the case the
   * reconnection logic exists for.
   */
  /*
   * A token for the client that stays connected, taken before the partition.
   *
   * Not the browser's stored session: tdc_session is Secure, so it is never
   * sent over the plain HTTP this suite runs against and an API request
   * context authenticates with nothing. A bearer token is what a program
   * outside the browser would use anyway.
   *
   * One sign-in for the whole file. The server allows eight per account per
   * quarter hour and it is right to — spending one per test is how a suite
   * starts reporting the application as broken when what broke was the suite.
   */
  const outside = await playwrightRequest.newContext({ baseURL: baseURL! });
  const signedIn = await outside.post("/api/auth/signin", {
    data: { username: "founder", password: "correct horse battery staple" },
  });
  expect(signedIn.ok(), "could not sign in to post from outside the browser").toBeTruthy();
  const authorised = { Authorization: `Bearer ${(await signedIn.json()).token}` };

  const rooms = await outside.get("/api/rooms", { headers: authorised });
  expect(rooms.ok(), "the channel list could not be read").toBeTruthy();
  const general = (await rooms.json()).find(
    (room: { slug?: string }) => room.slug === CHANNEL.toLowerCase(),
  );
  expect(general, "the channel this reads was not found over the API").toBeTruthy();

  await context.setOffline(true);

  /*
   * Something said while this client could not hear it, posted over the API
   * from outside the browser. This is what has to arrive on reconnection, and
   * arrive once.
   */
  const during = `during the partition ${Date.now().toString(36)}`;

  /*
   * Posted from a request context of its own, which is not the one the page
   * belongs to and so is not offline with it. That is what makes this a
   * partition rather than an outage: the world carries on while this client
   * cannot hear it.
   */
  const sent = await outside.post(`/api/rooms/${general.id}/messages`, {
    headers: authorised,
    data: { body: during },
  });
  expect(sent.ok(), "the message sent during the partition was refused").toBeTruthy();

  // And back.
  await context.setOffline(false);

  /*
   * The gap arrives. Generous, because reconnection is deliberately not
   * instant — an immediate retry storm is how a server under load stays under
   * load — so this waits for the behaviour rather than asserting a timing.
   */
  await expect(page.getByText(during, { exact: true })).toHaveCount(1, { timeout: 30_000 });

  /*
   * Nothing doubled. The message from before the partition is the one at risk:
   * a client that replays from too far back shows it a second time, and a
   * reader sees the conversation stutter.
   */
  expect(await timesShown(page, before), "a message from before the partition was repeated").toBe(
    1,
  );
  expect(await timesShown(page, during), "the message sent during the partition was repeated").toBe(
    1,
  );

  /*
   * And the conversation still works afterwards. A client that recovered its
   * history but not its connection has not recovered.
   */
  const after = `after the partition ${Date.now().toString(36)}`;
  await page.getByRole("textbox", { name: /message/i }).fill(after);
  await page.keyboard.press("Enter");
  await expect(page.getByText(after, { exact: true })).toHaveCount(1, { timeout: 15_000 });
});
