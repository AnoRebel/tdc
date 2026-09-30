import { expect, type Page } from "@playwright/test";

export const PASSWORD = "correct horse battery staple";

/**
 * Signs in through the sign-in page, as a person would, and dismisses the
 * introduction if this account has not seen it yet.
 */
export async function signIn(page: Page, username: string): Promise<void> {
  await page.goto("/signin");
  await page.getByLabel("Username or email").fill(username);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/signin"));

  // Waited for rather than asked about: the tour renders a moment after the
  // page, and isVisible() answers before it has.
  const tour = page.locator(".driver-popover");
  await tour.waitFor({ state: "visible", timeout: 5000 }).catch(() => {});
  if (await tour.count()) {
    await page.keyboard.press("Escape");
    await expect(tour).toHaveCount(0);
  }
}

/** Opens one of a project's views by its tab. */
export async function openTab(page: Page, name: string): Promise<void> {
  await page
    .getByRole("navigation", { name: "Ways of looking at this project" })
    .getByRole("link", { name, exact: true })
    .click();
}

/** Chooses an option from one of the shared Select controls. */
export async function choose(page: Page, label: string, option: string): Promise<void> {
  await page.getByRole("combobox", { name: label }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
  // Closed before anything else is touched: while the list is open it holds
  // the pointer, and a click meant for the page lands on its backdrop.
  await expect(page.getByRole("listbox")).toHaveCount(0);
}

/** A bearer token for an account, for fixtures made through the API. */
export async function tokenFor(page: Page, username: string): Promise<string> {
  const response = await page.request.post("/api/auth/signin", {
    data: { username, password: PASSWORD },
  });
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { token: string }).token;
}
