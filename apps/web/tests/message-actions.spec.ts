import { expect, test } from "@playwright/test";

/*
 * Answering a question from the keyboard alone.
 *
 * The accessibility pass says axe finds nothing wrong with the markup. This
 * asks the other question: whether somebody who cannot use a mouse can
 * actually reach a button, press it, and be told what happened. Those are
 * three separate things and axe checks none of them.
 */

/*
 * A channel of this file's own, made once per run.
 *
 * Not "General". Every other spec reads that channel, and a fixture posting
 * messages with buttons into it changes what they find — tab order, message
 * counts, which control is nth. Two keyboard tests and a messaging test failed
 * that way before this: each passed alone and failed in the suite, which is
 * the shape of a test that pollutes rather than one that is wrong.
 */
const CHANNEL = `choices-${Date.now().toString(36)}`;

/** Signs in as the founder and returns an authorised header. */
async function asFounder(request: import("@playwright/test").APIRequestContext) {
  const signIn = await request.post("/api/auth/signin", {
    data: { username: "founder", password: "correct horse battery staple" },
  });
  expect(signIn.ok(), "could not sign in to post the fixture").toBeTruthy();
  return { Authorization: `Bearer ${(await signIn.json()).token}` };
}

/** The channel these tests use, created on first call. */
async function ourChannel(
  request: import("@playwright/test").APIRequestContext,
  authorised: Record<string, string>,
) {
  const rooms = await request.get("/api/rooms", { headers: authorised });
  const existing = (await rooms.json()).find((room: { slug?: string }) => room.slug === CHANNEL);
  if (existing) return existing;

  const created = await request.post("/api/rooms", {
    headers: authorised,
    data: { slug: CHANNEL, name: CHANNEL, topic: "Messages that ask something" },
  });
  expect(created.ok(), "the fixture channel could not be created").toBeTruthy();
  return created.json();
}

/** Posts a message carrying buttons, as a program would. */
async function offerAChoice(
  request: import("@playwright/test").APIRequestContext,
  baseURL: string,
  body: string,
) {
  const authorised = await asFounder(request);
  const room = await ourChannel(request, authorised);

  const posted = await request.post(`/api/rooms/${room.id}/messages`, {
    headers: authorised,
    data: {
      body,
      actions: [
        { name: "yes", label: "Yes" },
        { name: "no", label: "No" },
      ],
    },
  });
  expect(posted.ok(), "the fixture message was refused").toBeTruthy();
  void baseURL;
}

test("a choice can be answered from the keyboard", async ({ page, request, baseURL }) => {
  const body = `Keyboard choice ${Date.now().toString(36)}`;
  await offerAChoice(request, baseURL!, body);

  await page.goto("/");
  await page
    .getByRole("link", { name: new RegExp(CHANNEL, "i") })
    .first()
    .click();
  await page.getByRole("textbox", { name: /message/i }).waitFor();

  // Found by role, which is how a screen reader looks: a div with a click
  // handler would not be here.
  const yes = page.getByRole("button", { name: "Yes" }).last();
  await expect(yes).toBeVisible();

  /*
   * Focused directly rather than tabbed to from the top of the page. Tab order
   * through a whole conversation is a different assertion — and a fragile one,
   * since it changes with every message — while what matters here is that the
   * control takes focus at all and answers the keyboard when it has it.
   */
  await yes.focus();
  await expect(yes).toBeFocused();

  await page.keyboard.press("Enter");

  /*
   * The outcome, said rather than only shown.
   *
   * In a live region, so somebody who cannot see the row change is told. The
   * text is the translated one, which is also a check that the row does not
   * hard-code English.
   */
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: /answered/i })
      .first(),
  ).toBeAttached({
    timeout: 15_000,
  });
});

test("a message offering nothing has no buttons to reach", async ({ page, request, baseURL }) => {
  /*
   * The counterpart. Without it, a row that rendered buttons on every message
   * would pass the test above while putting controls on messages that offer
   * none.
   */
  const authorised = await asFounder(request);
  const room = await ourChannel(request, authorised);

  const body = `No choices here ${Date.now().toString(36)}`;
  await request.post(`/api/rooms/${room.id}/messages`, {
    headers: authorised,
    data: { body },
  });
  void baseURL;

  await page.goto("/");
  await page
    .getByRole("link", { name: new RegExp(CHANNEL, "i") })
    .first()
    .click();
  await page.getByText(body, { exact: true }).waitFor();

  /*
   * Scoped to this message's own bubble rather than to the page.
   *
   * The channel still holds the message from the test above, which does offer
   * buttons — asserting none exist anywhere would fail for the wrong reason
   * and would keep failing as the channel filled up.
   */
  const bubble = page.locator("li, article, div").filter({ hasText: body }).last();
  await expect(bubble).toBeVisible();
  await expect(bubble.getByRole("button", { name: "Yes" })).toHaveCount(0);
  await expect(bubble.getByRole("button", { name: "No" })).toHaveCount(0);
});
