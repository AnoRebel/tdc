import { expect, test, type Page } from "@playwright/test";

import { registered, seesTheOther, signIn, tokenFor, tourSeen } from "./helpers";

/*
 * Calls Milestone 3: a protected link with a lobby.
 *
 * Run by scripts/demo-milestone-calls-3.sh against a fresh server with calls
 * on and guests allowed. The founder, in a call in a private channel, makes a
 * link with a passcode and a lobby that also lets in people without an
 * account. Kito — signed in on this server, but not in the channel — opens it,
 * gives the passcode, waits, is let in, and the two see each other. Wanjiru,
 * with no account at all, joins as a guest the same way. Neither can read the
 * channel: not its messages, not its members, not the channel itself.
 */

const run = Date.now().toString(36);
const KITO = `kito${run}`;
const GUEST = "Wanjiru (visitor)";
const PASSCODE = "4721";
const SECRET_LINE = `Budget figures for the board, ${run}`;

const callWindow = (page: Page) => page.locator("[data-call-window]");

/** What a room route answers this page's session, from inside the page. */
function statusOf(page: Page, path: string) {
  return page.evaluate(
    (path) => fetch(path, { credentials: "include" }).then((response) => response.status),
    path,
  );
}

/** That this page's session can read nothing of the conversation. */
async function cannotSeeTheConversation(page: Page, room: string) {
  for (const path of [
    `/api/rooms/${room}`,
    `/api/rooms/${room}/timeline`,
    `/api/rooms/${room}/members`,
  ]) {
    const status = await statusOf(page, path);
    expect(status, `${path} refuses this person`).toBeGreaterThanOrEqual(400);
  }
  await expect(page.locator("body")).not.toContainText(SECRET_LINE);
}

test("a protected link with a lobby admits somebody from elsewhere on the server, and a guest", async ({
  browser,
}) => {
  test.setTimeout(300_000);

  // The founder's private channel, with something in it nobody else may read.
  const founderContext = await browser.newContext();
  const founder = await founderContext.newPage();
  const founderToken = await tokenFor(founder, "founder");
  await tourSeen(founder, founderToken);
  await signIn(founder, "founder");
  const auth = { Authorization: `Bearer ${founderToken}` };
  const created = await founder.request.post("/api/rooms", {
    headers: auth,
    data: { slug: `board-${run}`, name: `board-${run}`, visibility: "private" },
  });
  expect(created.ok(), await created.text()).toBeTruthy();
  const room = ((await created.json()) as { id: string }).id;
  const posted = await founder.request.post(`/api/rooms/${room}/messages`, {
    headers: auth,
    data: { body: SECRET_LINE },
  });
  expect(posted.ok(), await posted.text()).toBeTruthy();
  // From a page of its own: a request from a page shares that page's cookies,
  // and signing Kito in there would sign the founder's browser out.
  const setup = await (await browser.newContext()).newPage();
  await tourSeen(setup, await registered(setup, KITO));

  // In a call there, with the camera on, the founder makes the link: a
  // passcode, a lobby, and guests allowed.
  await founder.goto(`/rooms/${room}`);
  await expect(founder.getByText(SECRET_LINE)).toBeVisible();
  await founder.getByRole("button", { name: "Start a call" }).click();
  const calls = callWindow(founder);
  await expect(calls.getByRole("button", { name: "Leave", exact: true })).toBeVisible();
  await calls.getByRole("button", { name: "Turn camera on" }).click();
  await calls.getByRole("button", { name: "Links to this call" }).click();
  await calls.getByLabel("Label (optional)").fill("Board visitors");
  await expect(calls.getByRole("switch", { name: "Let people in from a lobby" })).toBeChecked();
  // Forced: the switch's drawn track, which is what a person clicks, sits over
  // the checkbox it stands for.
  await calls
    .getByRole("switch", { name: "Let people without an account join" })
    .check({ force: true });
  await calls.getByRole("switch", { name: "Ask for a passcode" }).check({ force: true });
  await calls.getByLabel("Passcode (at least 4 characters)").fill(PASSCODE);
  await calls.getByRole("button", { name: "Make link" }).click();
  const made = calls
    .getByRole("status")
    .filter({ hasText: "it is shown only once" })
    .locator("code");
  await expect(made).toContainText("/call/");
  const link = new URL((await made.textContent())!.trim()).pathname;

  // Kito, signed in here but not in the channel, opens the link.
  const kitoContext = await browser.newContext();
  const kito = await kitoContext.newPage();
  await signIn(kito, KITO);
  await cannotSeeTheConversation(kito, room);
  await kito.goto(link);
  await expect(kito.getByRole("heading", { name: `board-${run}` })).toBeVisible();
  await expect(kito.getByText("Somebody in the call lets you in.")).toBeVisible();
  await kito.getByLabel("Passcode").fill(PASSCODE);
  await kito.getByRole("button", { name: "Join with video" }).click();
  await expect(kito.getByText("Waiting for somebody in the call to let you in")).toBeVisible();

  // The founder sees him waiting and lets him in.
  await expect(calls).toContainText("1 waiting to join");
  await calls.getByRole("button", { name: `Let ${KITO} in` }).click();
  await expect(kito.getByText("You are in the call.")).toBeVisible();
  await seesTheOther(kito);
  await seesTheOther(founder);
  // In the call, and still nowhere in the conversation.
  await cannotSeeTheConversation(kito, room);

  // Wanjiru has no account. She joins as a guest, with the passcode.
  const guestContext = await browser.newContext();
  const guest = await guestContext.newPage();
  await guest.goto(link);
  await expect(guest.getByRole("link", { name: "Sign in to join" })).toBeVisible();
  await guest.getByLabel("Your name, as people in the call will see it").fill(GUEST);
  await guest.locator("#guest-passcode").fill(PASSCODE);
  await guest.getByRole("button", { name: "Join as a guest" }).click();
  await expect(guest.getByText("Waiting for somebody in the call to let you in")).toBeVisible();

  await expect(calls).toContainText("1 waiting to join");
  await calls.getByRole("button", { name: `Let ${GUEST} in` }).click();
  await expect(guest.getByText("You are in the call.")).toBeVisible();
  await seesTheOther(guest);
  // Shown to everybody in the call as a guest.
  await expect(calls.locator("li", { hasText: GUEST })).toContainText("Guest");
  // A guest's pass opens the call and nothing of the conversation.
  await cannotSeeTheConversation(guest, room);

  for (const page of [guest, kito, founder]) {
    await callWindow(page).getByRole("button", { name: "Leave", exact: true }).click();
  }
  await expect(callWindow(founder)).toHaveCount(0);
});
