import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { createHmac } from "node:crypto";
import { createServer, type IncomingHttpHeaders, type Server } from "node:http";
import type { AddressInfo } from "node:net";

/*
 * An outgoing webhook's whole life, through the interface: made, delivered
 * to, failing, retried, and turned off — with an operator able to tell which
 * deliveries failed and why from the page alone.
 *
 * The receiver runs inside this test, on this machine, which is a private
 * address the server refuses by default; the test is the operator who allows
 * it, and puts the setting back afterwards. Retries are shortened by setting
 * too, so a schedule meant to span a day takes seconds.
 *
 * Chromium only: it exercises the server's queue far more than the engine,
 * and three engines would triple the time for nothing new.
 */

const password = "correct horse battery staple";

let receiver: Server;
let answer = 204;
const received: { headers: IncomingHttpHeaders; body: string }[] = [];

let token = "";
const changed: Record<string, string> = {
  "integrations.allow_private_addresses": "true",
  "integrations.webhook_retry_base_seconds": "1",
  "integrations.webhook_max_attempts": "3",
};

test.beforeAll(async ({ browserName, request }) => {
  test.skip(browserName !== "chromium", "the queue, not the engine, is under test");

  receiver = createServer((incoming, outgoing) => {
    let body = "";
    incoming.on("data", (chunk) => (body += chunk));
    incoming.on("end", () => {
      received.push({ headers: incoming.headers, body });
      outgoing.writeHead(answer).end(answer === 204 ? "" : "maintenance");
    });
  });
  await new Promise<void>((resolve) => receiver.listen(0, "127.0.0.1", resolve));

  const signedIn = await request.post("/api/auth/signin", {
    data: { username: "founder", password },
  });
  token = (await signedIn.json()).token;
  for (const [key, value] of Object.entries(changed)) {
    const set = await request.put(`/api/admin/settings/${key}`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { value },
    });
    expect(set.ok(), `setting ${key}`).toBeTruthy();
  }
});

test.afterAll(async ({ request }) => {
  if (!token) return;
  // Back to the defaults, so no other check runs against a server that
  // delivers to private addresses.
  const defaults: Record<string, string> = {
    "integrations.allow_private_addresses": "false",
    "integrations.webhook_retry_base_seconds": "60",
    "integrations.webhook_max_attempts": "12",
  };
  for (const [key, value] of Object.entries(defaults)) {
    await request.put(`/api/admin/settings/${key}`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { value },
    });
  }
  await new Promise((resolve) => receiver?.close(resolve));
});

test("a webhook is made, delivers, fails, retries and turns off, all visible", async ({
  page,
  request,
}) => {
  const auth = { Authorization: `Bearer ${token}` };
  const slug = `hooks-${Date.now().toString(36)}`;
  const room = await (
    await request.post("/api/rooms", { headers: auth, data: { slug, name: slug } })
  ).json();
  const endpoint = `http://127.0.0.1:${(receiver.address() as AddressInfo).port}/hook`;
  const label = `CI for ${slug}`;

  // Made through the form.
  await page.goto("/integrations");
  await page.getByLabel("Label").fill(label);
  await page.getByRole("combobox", { name: "Channel" }).click();
  await page.getByRole("option", { name: `#${slug}` }).click();
  await page.getByLabel("Endpoint").fill(endpoint);
  await page.getByRole("button", { name: "Create webhook" }).click();

  // The secret, once, said to be once.
  const notice = page.getByRole("status").filter({ hasText: "not shown again" });
  await expect(notice).toBeVisible();
  const secret = (await notice.locator("code").textContent())?.trim() ?? "";
  expect(secret).toMatch(/^whsec_/);

  const audit = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(audit.violations.map((violation) => `${violation.id}: ${violation.help}`)).toEqual([]);

  await page.getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("status").filter({ hasText: "not shown again" })).toHaveCount(0);

  // Delivered, and verifiable with the secret the page showed.
  await request.post(`/api/rooms/${room.id}/messages`, {
    headers: auth,
    data: { body: "Build 41 passed" },
  });
  await expect.poll(() => received.length, { timeout: 10_000 }).toBeGreaterThan(0);
  const first = received[0]!;
  const [, created, signature] = /t=(\d+),v1=([0-9a-f]+)/.exec(
    String(first.headers["tdc-signature-hmac"]),
  )!;
  expect(createHmac("sha256", secret).update(`${created}.${first.body}`).digest("hex")).toBe(
    signature,
  );

  const card = page.locator(`[data-webhook="${label}"]`);
  await card.getByRole("button", { name: "Delivery history" }).click();
  const history = card.getByRole("table");
  await expect(history.getByRole("row").filter({ hasText: "Delivered" })).toHaveCount(1);
  await expect(history).toContainText("204");

  // The receiver starts refusing; the page shows each failed attempt and why.
  answer = 503;
  await request.post(`/api/rooms/${room.id}/messages`, {
    headers: auth,
    data: { body: "Build 42 failed" },
  });

  await expect
    .poll(
      async () => {
        await page.reload();
        // Counted once the list has rendered, not while the page is blank.
        await card.waitFor();
        return card.getByText("Turned off", { exact: true }).count();
      },
      { timeout: 20_000, intervals: [1_000] },
    )
    .toBe(1);

  await expect(card).toContainText("a delivery failed all 3 attempts");
  await card.getByRole("button", { name: "Delivery history" }).click();
  const failed = card.getByRole("table").getByRole("row").filter({ hasText: "Failed" });
  await expect(failed).toHaveCount(3);
  await expect(failed.first()).toContainText("503");
  await expect(failed.first()).toContainText("maintenance");

  // And removed, after being asked.
  await card.getByRole("button", { name: `Remove ${label}` }).click();
  await page.getByRole("button", { name: "Remove", exact: true }).click();
  await expect(card).toHaveCount(0);
});
