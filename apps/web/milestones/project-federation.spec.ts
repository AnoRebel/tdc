import { expect, test, type BrowserContext, type Page } from "@playwright/test";

import { PASSWORD, signIn, tokenFor } from "./helpers";

/*
 * A project shared between two servers, in a real browser on each.
 *
 * Run by scripts/demo-milestone-project-federation-5.sh, which starts both
 * servers with federation on and each trusting the other — the operator's part,
 * from the hosting guide. Everything here is what the projects guide tells
 * people to do: an administrator on A opens a project and adds somebody from B,
 * who then works in it from B. A move made stale by somebody on A is refused
 * and put back, saying why; and when A stops answering, B says so.
 */

const A = process.env.TDC_TEST_URL_A ?? "http://localhost:8441";
const B = process.env.TDC_TEST_URL_B ?? "http://localhost:8442";
// The names the servers sign as, which is how each knows the other.
const HOME = new URL(process.env.TDC_CANONICAL_A ?? "http://a.localhost:8441").hostname;
const ELSEWHERE = new URL(process.env.TDC_CANONICAL_B ?? "http://b.localhost:8442").hostname;
const RUN = (process.env.TDC_FEDERATION_RUN ?? Date.now().toString(36)).toLowerCase();

const KEY = `F${
  RUN.replace(/[^a-z]/g, "")
    .slice(-4)
    .toUpperCase() || "ED"
}`;
const REPLICA = `${KEY}@${HOME}`;
const PERSON = `dalila${RUN.replace(/[^a-z0-9]/g, "")}`;

test.describe.configure({ mode: "serial" });

let home: { context: BrowserContext; page: Page; token: string };
let away: { context: BrowserContext; page: Page; token: string };

async function call<T>(
  page: Page,
  token: string,
  method: string,
  path: string,
  data?: unknown,
): Promise<T> {
  const response = await page.request.fetch(path, {
    method,
    headers: { Authorization: `Bearer ${token}` },
    data,
  });
  expect(response.ok(), `${method} ${path}: ${await response.text()}`).toBeTruthy();
  const text = await response.text();
  return (text ? JSON.parse(text) : null) as T;
}

/** Moves a card one column along from the keyboard, as the board offers. */
async function move(page: Page, card: string, from: string, direction: "ArrowLeft" | "ArrowRight") {
  const handle = page.getByRole("button", { name: `Move ${card}, now in ${from}` });
  await handle.focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press(direction);
  await page.keyboard.press("Enter");
}

const column = (page: Page, state: string, card: string) =>
  page.locator(`[data-state="${state}"] [data-card="${card}"]`);

test.beforeAll(async ({ browser }) => {
  test.setTimeout(120_000);
  const homeContext = await browser.newContext({ baseURL: A });
  const homePage = await homeContext.newPage();
  await signIn(homePage, "founder");
  home = { context: homeContext, page: homePage, token: await tokenFor(homePage, "founder") };

  const awayContext = await browser.newContext({ baseURL: B });
  const awayPage = await awayContext.newPage();
  const registered = await awayPage.request.post("/api/auth/register", {
    data: { username: PERSON, password: PASSWORD },
  });
  expect(registered.ok(), await registered.text()).toBeTruthy();
  await signIn(awayPage, PERSON);
  away = { context: awayContext, page: awayPage, token: await tokenFor(awayPage, PERSON) };
});

test.afterAll(async () => {
  await home?.context.close();
  await away?.context.close();
});

test("an administrator on A opens a project to other servers and adds somebody from B", async () => {
  const { page, token } = home;
  await call(page, token, "POST", "/api/projects", {
    key: KEY,
    name: "Website",
    visibility: "private",
  });
  await call(page, token, "POST", `/api/projects/${KEY}/issues`, {
    title: "Seen from both servers",
  });

  await page.goto(`/projects/${KEY}/settings`);
  // The switch draws over its own checkbox: clicked where a person would.
  const open = page.getByRole("switch", { name: "Open to other servers" });
  await open.locator("xpath=..").click();
  await expect(open).toBeChecked();

  await page.getByLabel("Username").fill(`${PERSON}@${ELSEWHERE}`);
  await page.getByRole("button", { name: "Add", exact: true }).click();
  const member = page.getByRole("listitem").filter({ hasText: `${PERSON}@${ELSEWHERE}` });
  await expect(member).toBeVisible();
  await expect(member).toContainText("Elsewhere");
});

test("on B, the project is among theirs, marked as living on A", async () => {
  const { page } = away;
  await expect
    .poll(
      async () => {
        await page.goto("/projects");
        return page
          .getByRole("link", { name: new RegExp(REPLICA) })
          .first()
          .waitFor({ timeout: 3000 })
          .then(() => true)
          .catch(() => false);
      },
      { timeout: 60_000 },
    )
    .toBe(true);

  await page.goto(`/projects/${REPLICA}/board`);
  await expect(page.getByText(`Shared from ${HOME}.`)).toBeVisible();
  await expect(column(page, "To do", `${REPLICA}-1`)).toBeVisible();
});

test("a card moved on B waits for A, lands, and A has it there too", async () => {
  const { page } = away;
  const card = `${REPLICA}-1`;
  // Held a moment on its way, so what the board shows meanwhile can be seen.
  let release: () => void = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route("**/api/issues/*/state", async (route) => {
    await held;
    await route.continue();
  });

  await move(page, card, "To do", "ArrowRight");
  const moving = column(page, "In progress", card);
  await expect(moving).toHaveAttribute("aria-busy", "true");
  release();
  await expect(moving).not.toHaveAttribute("aria-busy");
  await page.unroute("**/api/issues/*/state");

  await home.page.goto(`/projects/${KEY}/board`);
  await expect(column(home.page, "In progress", `${KEY}-1`)).toBeVisible();
});

test("a move somebody on A made stale is refused, and the card goes where it really is", async () => {
  const { page, token } = away;
  const card = `${REPLICA}-1`;
  const project = await call<{ states: { id: string; name: string }[] }>(
    home.page,
    home.token,
    "GET",
    `/api/projects/${KEY}`,
  );
  const done = project.states.find((state) => state.name === "Done")!;
  await call(home.page, home.token, "POST", `/api/issues/${KEY}-1/state`, { stateId: done.id });
  // B has heard of it; its board, open all along, has not been redrawn.
  await expect
    .poll(
      async () =>
        (
          await call<{ state: { name: string } }>(
            page,
            token,
            "GET",
            `/api/issues/${encodeURIComponent(card)}`,
          )
        ).state.name,
      { timeout: 30_000 },
    )
    .toBe("Done");
  await expect(column(page, "In progress", card)).toBeVisible();

  await move(page, card, "In progress", "ArrowLeft");
  await expect(page.getByRole("alert")).toContainText(`Somebody else moved ${card} first`);
  await expect(column(page, "Done", card)).toBeVisible();
});

test("on B, a public project on A is added there, read, and joined once A lets her in", async () => {
  const { page } = away;
  const open = `O${KEY.slice(1)}`;
  await call(home.page, home.token, "POST", "/api/projects", {
    key: open,
    name: "Open roadmap",
    visibility: "public",
  });
  await call(home.page, home.token, "PUT", `/api/projects/${open}/federation`, { federated: true });
  await call(home.page, home.token, "POST", `/api/projects/${open}/issues`, {
    title: "Anybody may read this",
  });

  await page.goto("/projects");
  await page.getByRole("button", { name: "Add from another server" }).click();
  await page
    .getByLabel("Server", { exact: true })
    .fill(new URL(A).origin.replace("localhost", HOME));
  await page.getByRole("button", { name: "Look" }).click();
  await page.getByRole("button", { name: "Add Open roadmap to this server" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Added." })).toBeVisible();

  const followed = `${open}@${HOME}`;
  await expect
    .poll(
      async () => {
        await page.goto(`/projects/${followed}/board`);
        return column(page, "To do", `${followed}-1`)
          .waitFor({ timeout: 3000 })
          .then(() => true)
          .catch(() => false);
      },
      { timeout: 60_000 },
    )
    .toBe(true);
  // Read, not changed: she is not in it.
  await expect(page.getByRole("button", { name: `Move ${followed}-1, now in To do` })).toHaveCount(
    0,
  );

  await page.getByRole("button", { name: "Ask to join" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: `Asked. ${HOME} decides` }),
  ).toBeVisible();

  await home.page.goto(`/projects/${open}/board`);
  await home.page.getByRole("link", { name: "1 person waiting to join" }).click();
  const waiting = home.page.getByRole("region", { name: "Waiting to join" });
  await expect(waiting).toContainText(`${PERSON}@${ELSEWHERE}`);
  await waiting.getByRole("button", { name: "Let in" }).click();
  await expect(home.page.getByText(`${PERSON}@${ELSEWHERE} is a member now.`)).toBeVisible();

  await expect
    .poll(
      async () => {
        await page.goto(`/projects/${followed}/board`);
        return page
          .getByRole("button", { name: `Move ${followed}-1, now in To do` })
          .waitFor({ timeout: 3000 })
          .then(() => true)
          .catch(() => false);
      },
      { timeout: 60_000 },
    )
    .toBe(true);
});

test("when A stops answering, B says so, and a move is put back saying why", async () => {
  const pid = Number(process.env.TDC_SERVER_A_PID);
  test.skip(!pid, "the script that started A says which process it is");
  const { page } = away;
  const card = `${REPLICA}-1`;
  await page.goto(`/projects/${REPLICA}/board`);
  await expect(column(page, "Done", card)).toBeVisible();
  process.kill(pid, "SIGTERM");
  await expect
    .poll(
      async () =>
        home.page.request
          .get("/api/health", { timeout: 2000 })
          .then((response) => response.ok())
          .catch(() => false),
      { timeout: 30_000 },
    )
    .toBe(false);

  await move(page, card, "Done", "ArrowLeft");
  await expect(page.getByRole("alert")).toContainText(`${HOME} could not be reached`);
  await expect(column(page, "Done", card)).toBeVisible();

  await page.reload();
  await expect(
    page.getByRole("status").filter({ hasText: `${HOME} has not answered since` }),
  ).toBeVisible();
});
