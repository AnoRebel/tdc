import { expect, test } from "@playwright/test";

/*
 * The Milestone 10a demonstration, web half.
 *
 * A program authenticates with a token, posts a message offering two choices,
 * and a person presses one of them in a browser. The program hears about it.
 *
 * The other press comes from the desktop client, and is demonstrated where the
 * desktop actually lives: apps/desktop/internal/api, driving the Go service
 * the Wails binding calls. The desktop does not speak HTTP, so driving a
 * browser against its built frontend would exercise a transport it never uses
 * and prove the wrong thing convincingly.
 *
 * Run by scripts/milestone-10a.sh, which brings up a server, runs this, then
 * runs the desktop half, and reads the channel back.
 */

const password = "correct horse battery staple";

test("a person presses a choice in the web client", async ({ page, request, baseURL }) => {
  /*
   * The program, holding a token rather than somebody's password. Scoped to
   * posting and reading: it has no business doing anything else.
   */
  const asOwner = await request.post("/api/auth/signin", {
    data: { username: "founder", password },
  });
  expect(asOwner.ok(), "could not sign in to issue the program's token").toBeTruthy();
  const session = { Authorization: `Bearer ${(await asOwner.json()).token}` };

  const issued = await request.post("/api/me/tokens", {
    headers: session,
    data: { label: "milestone bot", scope: ["channel.post", "channel.read"] },
  });
  expect(issued.ok(), "the program's token was refused").toBeTruthy();
  const asProgram = { Authorization: `Bearer ${(await issued.json()).token}` };

  // Its own channel, so the demonstration is legible afterwards.
  const slug = `milestone-${Date.now().toString(36)}`;
  const channel = await request.post("/api/rooms", {
    headers: session,
    data: { slug, name: slug, topic: "Milestone 10a" },
  });
  expect(channel.ok(), "the channel could not be created").toBeTruthy();
  const room = await channel.json();

  // The question, asked by the program with the token it was given.
  const asked = await request.post(`/api/rooms/${room.id}/messages`, {
    headers: asProgram,
    data: {
      body: "Deploy build 41 to production?",
      clientToken: crypto.randomUUID(),
      actions: [
        { name: "deploy", label: "Deploy" },
        { name: "hold", label: "Not yet" },
      ],
    },
  });
  expect(asked.ok(), "the program could not ask the question").toBeTruthy();
  const question = await asked.json();

  /*
   * And a person presses one, in a browser, with a mouse — which is the half
   * of this that an HTTP client cannot demonstrate.
   */
  await page.goto("/");
  await page
    .getByRole("link", { name: new RegExp(slug, "i") })
    .first()
    .click();
  await page.getByRole("textbox", { name: /message/i }).waitFor();

  const deploy = page.getByRole("button", { name: "Deploy" }).last();
  await expect(deploy).toBeVisible();
  await deploy.click();

  // The program hears about it, in the channel's own log.
  await expect(async () => {
    const timeline = await request.get(`/api/rooms/${room.id}/timeline`, { headers: asProgram });
    const events = await timeline.json();
    const press = events.find(
      (event: { type: string; content?: { action?: string; pressed?: string } }) =>
        event.type === "action.pressed" && event.content?.pressed === question.id,
    );
    expect(press, "the press did not reach the program").toBeTruthy();
    expect(press.content.action).toBe("deploy");
  }).toPass({ timeout: 15_000 });

  /*
   * What the desktop half needs, written where a shell script can read it.
   * The second press is made against this same message, so the two arrive on
   * one question rather than on two that happen to look alike.
   */
  // eslint-disable-next-line no-console
  console.log(
    `MILESTONE_ROOM=${room.id}\nMILESTONE_MESSAGE=${question.id}\nMILESTONE_SLUG=${slug}`,
  );
  void baseURL;
});
