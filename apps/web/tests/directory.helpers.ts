import { expect, type APIRequestContext, type Page, test } from "@playwright/test";

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
  const founder = await tokenFor(request, "founder");
  const toggled = await request.put("/api/admin/features/directory", {
    headers: founder,
    data: { enabled: true },
  });
  expect(toggled.status(), "turning the directory on").toBe(204);
  return founder;
}

/*
 * An ordinary account, registered on first use and reused after.
 *
 * Signed in first and registered only when that fails: a registration attempt
 * for a name already taken still counts against the allowance, and a suite
 * that re-registered everybody per test would spend it in one run.
 */
export async function account(request: APIRequestContext, username: string) {
  const existing = await request.post("/api/auth/signin", { data: { username, password } });
  if (existing.ok()) return { Authorization: `Bearer ${(await existing.json()).token}` };

  const registered = await request.post("/api/auth/register", { data: { username, password } });
  expect(registered.status(), `registering ${username}: ${await registered.text()}`).toBe(201);
  return tokenFor(request, username);
}

/** Signs somebody in through the form, and dismisses the introduction. */
export async function signInAs(page: Page, username: string) {
  await page.goto("/signin");
  await page.getByRole("button", { name: "Sign in", exact: true }).waitFor();
  await page.getByLabel(/username or email/i).fill(username);
  await page.getByLabel(/^password/i).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));

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
