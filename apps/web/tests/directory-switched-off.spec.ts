import { expect, test } from "@playwright/test";

import {
  account,
  founderWithDirectory,
  onlyInDirectoryProjects,
  people,
  signInAs,
} from "./directory.helpers";

/*
 * Switched off, the directory is gone from the interface as well as the API:
 * no link to it, and a bookmarked directory page lands on the start page
 * rather than on a form whose every request would be refused. Switched on
 * again, it is back with nothing to redo.
 *
 * Restores the directory before it finishes, whatever happens, because every
 * other directory check on this server expects it on.
 */

onlyInDirectoryProjects();
test.describe.configure({ mode: "serial" });

test("switching the directory off removes it from the interface", async ({ page, request }) => {
  const founder = await founderWithDirectory(request);
  const asSomebody = await account(request, people.closed);

  await signInAs(page, people.closed);
  // Counted rather than looked at: on a phone the sidebar is drawn off the
  // screen until opened, and what matters is whether the link is there at all.
  const link = page.getByRole("link", { name: "Directory", exact: true });
  await expect(link).toHaveCount(1);

  try {
    const off = await request.put("/api/admin/features/directory", {
      headers: founder,
      data: { enabled: false },
    });
    expect(off.status()).toBe(204);

    // What the application knew about the server is read again on a load.
    await page.reload();
    await expect(page.getByRole("navigation").first()).toBeVisible();
    await expect(link).toHaveCount(0);

    for (const path of ["/directory", "/directory/profile", "/directory/engagements"]) {
      await page.goto(path);
      // Bounded well inside the test's own limit, so a failure here still
      // leaves time for the directory to be switched back on below.
      await page.waitForURL((url) => !url.pathname.startsWith("/directory"), {
        timeout: 15_000,
      });
    }
    expect((await request.get("/api/directory/me", { headers: asSomebody })).status()).toBe(404);
  } finally {
    await request.put("/api/admin/features/directory", {
      headers: founder,
      data: { enabled: true },
    });
  }

  await page.reload();
  await expect(link).toHaveCount(1);
  await page.goto("/directory");
  await expect(page).toHaveURL(/\/directory$/);
  await expect(page.getByRole("heading", { name: "Directory", exact: true })).toBeVisible();
});
