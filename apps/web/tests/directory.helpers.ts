import {
  expect,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
  test,
} from "@playwright/test";

/*
 * What the directory's browser checks share.
 *
 * Accounts are made over the API rather than through the registration form:
 * the form has its own checks, and a directory check that registered through
 * it would spend the server's registration allowance — five an hour from one
 * address — on something it is not testing. The people these checks are
 * about sign in through the real form, because that is where they start.
 */

export const password = "correct horse battery staple";

/*
The people every directory check shares, named once per run.

Shared across files rather than made per check, because the server allows five
registrations an hour from one address and the checks between them need more
than five people otherwise. Fresh per run, so a second run against the same
server does not find last run's profiles already written — but the same in
every browser within one run, from the configuration, so the second and third
engines sign in as the people the first registered rather than spending the
allowance again.
*/
const run = process.env.TDC_DIRECTORY_RUN ?? Date.now().toString(36);
export const people = {
  developer: `amani${run}`,
  client: `baraka${run}`,
  closed: `zawadi${run}`,
};

/** Skips a spec unless one of the directory projects is running it. */
export function onlyInDirectoryProjects() {
  // eslint-disable-next-line no-empty-pattern -- Playwright passes fixtures first
  test.beforeEach(({}, testInfo) => {
    testInfo.skip(
      !testInfo.project.name.startsWith("directory"),
      "run by the directory projects, which bring their own accounts",
    );
  });
}

/** Signs in over the API and returns a bearer header. */
export async function tokenFor(request: APIRequestContext, username: string) {
  const response = await request.post("/api/auth/signin", { data: { username, password } });
  expect(response.ok(), `signing in as ${username}`).toBeTruthy();
  return { Authorization: `Bearer ${(await response.json()).token}` };
}

/**
 * The founder, set up if this is a fresh server, with the directory on.
 *
 * Turning the directory on is part of the check rather than assumed: it is off
 * by default, and a server where somebody forgot to turn it on should fail
 * here with a clear message rather than on a missing link three steps later.
 */
export async function founderWithDirectory(request: APIRequestContext) {
  await request.post("/api/setup", { data: { username: "founder", password } });
  const founder = await account(request, "founder");
  const toggled = await request.put("/api/admin/features/directory", {
    headers: founder,
    data: { enabled: true },
  });
  expect(toggled.status(), "turning the directory on").toBe(204);
  return founder;
}

/*
 * No enquiry from the client to anybody left waiting.
 *
 * One person may have only one unanswered enquiry to another, and an earlier
 * check — or the same check in an earlier browser — may have left one. Only
 * signs in when the client already exists, so a check that registers them
 * through the form still finds them unregistered.
 */
export async function noEnquiryPending(request: APIRequestContext) {
  const headers = await existing(request, people.client);
  if (!headers) return;
  const sent = await request.get("/api/directory/me/enquiries?box=sent", { headers });
  for (const enquiry of (await sent.json()) as { id: string; status: string }[]) {
    if (enquiry.status !== "pending") continue;
    await request.post(`/api/directory/enquiries/${enquiry.id}/withdraw`, { headers });
  }
}

/*
 * A bearer header for somebody who already has an account, or null when they
 * have none yet — without registering them. Reuses this run's session when
 * there is one, because sign-ins are limited per account.
 */
export async function existing(request: APIRequestContext, username: string) {
  const known = tokens.get(username);
  if (known) return known;
  const signedIn = await request.post("/api/auth/signin", { data: { username, password } });
  if (!signedIn.ok()) return null;
  const headers = { Authorization: `Bearer ${(await signedIn.json()).token}` };
  tokens.set(username, headers);
  return headers;
}

/*
 * A second phone, of the same kind the check is running on.
 *
 * The device each directory project emulates, rather than one fixed phone: a
 * Chromium phone's settings do not apply in Firefox, which has no mobile mode,
 * and the point of running in three engines is that both people are in the
 * one being checked.
 */
export async function secondPhone(browser: Browser) {
  const {
    defaultBrowserType: _engine,
    baseURL,
    viewport,
    userAgent,
    deviceScaleFactor,
    isMobile,
    hasTouch,
  } = test.info().project.use;
  return browser.newContext({
    baseURL,
    viewport,
    userAgent,
    deviceScaleFactor,
    isMobile,
    hasTouch,
  });
}

/*
 * An ordinary account, registered on first use and reused after.
 *
 * Signed in first and registered only when that fails: a registration attempt
 * for a name already taken still counts against the allowance, and a suite
 * that re-registered everybody per test would spend it in one run.
 */
export async function account(request: APIRequestContext, username: string) {
  const known = tokens.get(username);
  if (known) return known;

  const existing = await request.post("/api/auth/signin", { data: { username, password } });
  if (existing.ok()) {
    const token = { Authorization: `Bearer ${(await existing.json()).token}` };
    tokens.set(username, token);
    return token;
  }

  const registered = await request.post("/api/auth/register", { data: { username, password } });
  expect(registered.status(), `registering ${username}: ${await registered.text()}`).toBe(201);
  const token = await tokenFor(request, username);
  tokens.set(username, token);
  return token;
}

/**
 * Where somebody is, set the way settings sets it.
 *
 * Not part of the profile: a location belongs to the account and the profile
 * only shows it. Public, so a search by place finds the person whatever their
 * profile's own visibility, which is what the checks here vary.
 */
export async function placeIn(
  request: APIRequestContext,
  headers: { Authorization: string },
  place: string,
) {
  const set = await request.put("/api/me/location", {
    headers,
    data: { mode: "manual", place, visibility: "public" },
  });
  expect(set.ok(), `setting ${place}: ${await set.text()}`).toBeTruthy();
}

/*
Tokens already issued in this run, by username.

Kept so a second check needing the same person reuses their session: sign-ins
are limited per account and per address, and a suite that signed in afresh for
every check would spend the allowance on repetition.
*/
const tokens = new Map<string, { Authorization: string }>();

/*
Browser sessions already made in this run, by username.

The same reason as the tokens above, for the form: each person signs in through
it once per browser engine, and every later check carries the session over. A
suite that typed the password into every check would spend an account's eight
sign-ins a quarter hour before reaching the last of them.
*/
const sessions = new Map<string, Awaited<ReturnType<BrowserContext["cookies"]>>>();

/** Signs somebody in through the form, and dismisses the introduction. */
export async function signInAs(page: Page, username: string) {
  const saved = sessions.get(username);
  if (saved) {
    await page.context().clearCookies();
    await page.context().addCookies(saved);
    await page.goto("/");
    // A session the server has since ended sends the page to sign in; then
    // the form, as if there had been none.
    const outcome = await Promise.race([
      page
        .locator('[data-tour="channels"]')
        .waitFor({ state: "attached", timeout: 15_000 })
        .then(() => "signed in"),
      page
        .waitForURL((url) => url.pathname.startsWith("/signin"), { timeout: 15_000 })
        .then(() => "signed out"),
    ]).catch(() => "signed out");
    if (outcome === "signed in") {
      await dismissTour(page);
      return;
    }
  }

  await page.goto("/signin");
  await page.getByRole("button", { name: "Sign in", exact: true }).waitFor();
  await page.getByLabel(/username or email/i).fill(username);
  await page.getByLabel(/^password/i).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));
  sessions.set(username, await page.context().cookies());
  await dismissTour(page);
}

async function dismissTour(page: Page) {
  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 4000 }).catch(() => {});
  if (await tour.count()) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }
}

/**
 * Asserts nothing on the page is wider than the phone.
 *
 * The failure this catches is a form that fits on a laptop and makes a phone
 * scroll sideways to reach its submit button.
 */
export async function fitsThePhone(page: Page) {
  const overflow = await page.evaluate(() => {
    const width = document.documentElement.clientWidth;
    return [...document.querySelectorAll("body *")]
      .filter((element) => {
        const box = element.getBoundingClientRect();
        return box.width > 0 && box.right > width + 1;
      })
      .map((element) => `${element.tagName.toLowerCase()}.${element.className}`.slice(0, 80))
      .slice(0, 5);
  });
  expect(overflow, "elements wider than the phone").toEqual([]);
}
