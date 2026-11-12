import { expect, type Browser, type Page } from "@playwright/test";

/*
 * Fixtures for the projects checks, made without spending sign-ins.
 *
 * The server allows eight sign-ins per account per quarter hour, and this suite
 * runs on three engines. So fixtures are made from inside a page that is
 * already signed in — the stored session — with the same cookie the
 * application itself sends, rather than by signing in again for a token.
 */

export const PASSWORD = "correct horse battery staple";

/** A name nobody else's run has used: projects and channels outlive a run. */
export function unique(prefix: string): string {
  return `${prefix}${Date.now().toString(36).slice(-6)}`.toUpperCase();
}

/** Calls the API as whoever the page is signed in as. */
export async function api<T = unknown>(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; body: T }> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const response = await fetch(path, {
        method,
        credentials: "include",
        headers: body === undefined ? {} : { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const text = await response.text();
      return { status: response.status, body: text ? JSON.parse(text) : null };
    },
    { method, path, body },
  );
}

/** As api, and the call must succeed. */
export async function must<T = unknown>(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const answer = await api<T>(page, method, path, body);
  expect(answer.status, `${method} ${path}: ${JSON.stringify(answer.body)}`).toBeLessThan(300);
  return answer.body;
}

/**
 * A second person, signed in once for the whole file.
 *
 * Registered only the first time: registrations are limited per address per
 * hour, and three engines would otherwise spend three of five.
 */
export async function secondPerson(browser: Browser, baseURL: string, username: string) {
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();
  const registered = await page.request.post("/api/auth/register", {
    data: { username, password: PASSWORD },
  });
  expect([201, 409], await registered.text()).toContain(registered.status());

  await page.goto("/signin");
  await page.getByLabel("Username or email").fill(username);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));
  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 5000 }).catch(() => {});
  if (await tour.count()) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }
  return { context, page };
}

/** A project of the signed-in person's, with its states by name. */
export async function project(page: Page, key: string, visibility = "public") {
  const made = await must<{ states: { id: string; name: string }[] }>(
    page,
    "POST",
    "/api/projects",
    {
      key,
      name: `Project ${key}`,
      visibility,
    },
  );
  const state = (name: string) => made.states.find((s) => s.name === name)!.id;
  return { key, state };
}
