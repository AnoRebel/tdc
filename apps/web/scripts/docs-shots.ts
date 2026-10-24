/**
 * Every screenshot in docs/images, and how each is taken.
 *
 * One entry per image: the guide it belongs to, its file name, and what to do
 * in the application before the picture. A guide refers to an image as
 * images/<guide>/<name>.webp; renaming one here means renaming it there.
 *
 * Shots find things the way a person would — by role and visible name — so a
 * change to the interface that breaks one is also a change somebody reading
 * the guide would notice.
 */
import type { Locator, Page } from "@playwright/test";

import type { Studio } from "./docs-screenshots";

export interface Shot {
  guide: string;
  name: string;
  take(studio: Studio): Promise<Buffer>;
}

interface OpenOptions {
  phone?: boolean;
  dark?: boolean;
  permissions?: string[];
}

/** A page signed in as somebody (or nobody), opened at a path and settled. */
async function open(studio: Studio, user: string | null, path: string, options: OpenOptions = {}) {
  const context = await studio.as(user, options);
  const page = await context.newPage();
  await page.goto(path);
  await settle(page);
  return page;
}

/** Waits for the page to stop loading, and hides what a still picture should not show. */
async function settle(page: Page) {
  await page.waitForLoadState("networkidle").catch(() => {});
  await page
    .locator(".animate-pulse")
    .first()
    .waitFor({ state: "detached", timeout: 5000 })
    .catch(() => {});
  await page.addStyleTag({ content: "* { caret-color: transparent !important; }" });
  await page.waitForTimeout(400);
  // A dialog still fading in is a dialog half there.
  await page
    .waitForFunction(
      () => document.getAnimations().every((animation) => animation.playState !== "running"),
      undefined,
      { timeout: 3000 },
    )
    .catch(() => {});
  await page.waitForTimeout(300);
}

const room = (studio: Studio, slug: string) => `/rooms/${studio.seeded.rooms[slug]}`;

/** The section of a page under a heading. */
function section(page: Page, heading: string): Locator {
  return page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: heading, exact: true }) })
    .first();
}

/**
 * Part of the page, with a margin of the page around it so the crop does not
 * cut through a border. Something taller than the window is shown from its
 * top, as far as the window reaches.
 */
async function region(page: Page, target: Locator, margin = 16): Promise<Buffer> {
  await target.evaluate((element) => element.scrollIntoView({ block: "start" }));
  await page.waitForTimeout(300);
  const box = await target.boundingBox();
  const viewport = page.viewportSize()!;
  if (!box) throw new Error("nothing to photograph");
  const x = Math.max(0, box.x - margin);
  const y = Math.max(0, box.y - margin);
  const right = Math.min(viewport.width, box.x + box.width + margin);
  const bottom = Math.min(viewport.height, box.y + box.height + margin);
  return page.screenshot({ clip: { x, y, width: right - x, height: bottom - y } });
}

/** The whole window. */
const window = (page: Page) => page.screenshot();

/** An API request as one of the seeded people, for what a shot sets up off screen. */
async function as(studio: Studio, who: string, method: string, path: string, body?: unknown) {
  const response = await fetch(new URL(path, studio.base), {
    method,
    headers: {
      Authorization: `Bearer ${studio.seeded.people[who]!.token}`,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) throw new Error(`${who}: ${method} ${path} → ${response.status} ${text}`);
  return (text ? JSON.parse(text) : {}) as Record<string, unknown>;
}

const media = { permissions: ["microphone", "camera"] };

/*
 * A call in #general: Neema starts it, Juma and Zawadi join by voice, and the
 * three are connected by the time this returns.
 */
async function callInGeneral(studio: Studio) {
  // An earlier shot's call ends once the server notices its people gone,
  // within about fifteen seconds; each shot starts its own.
  for (let tries = 0; tries < 60; tries++) {
    const now = await fetch(
      new URL(`/api/rooms/${studio.seeded.rooms.general}/call`, studio.base),
      {
        headers: { Authorization: `Bearer ${studio.seeded.people.neema!.token}` },
      },
    );
    const call = now.ok ? ((await now.json()) as { participants?: unknown[] } | null) : null;
    if (!call?.participants?.length) break;
    await Bun.sleep(1000);
  }
  const host = await open(studio, "neema", room(studio, "general"), media);
  await host.getByRole("button", { name: "Start a call" }).click();
  for (const who of ["juma", "zawadi"]) {
    const guest = await open(studio, who, room(studio, "general"), media);
    await guest
      .locator("[data-call-banner]")
      .getByRole("button", { name: "Join", exact: true })
      .click();
  }
  await host.locator("[data-call-window] li").nth(2).waitFor();
  await host.waitForTimeout(2500);
  return host;
}

/** The call window, large. */
async function expanded(page: Page) {
  await page.keyboard.press("Shift+F");
  await settle(page);
}

export const shots: Shot[] = [
  /* ----------------------------------------------------------- using-tdc */
  {
    guide: "using-tdc",
    name: "sign-in",
    async take(studio) {
      const page = await open(studio, null, "/signin");
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "sessions",
    async take(studio) {
      const page = await open(studio, "neema", "/settings");
      return region(page, section(page, "Signed-in devices"));
    },
  },
  {
    guide: "using-tdc",
    name: "channel",
    async take(studio) {
      return window(await open(studio, "neema", room(studio, "golang")));
    },
  },
  {
    guide: "using-tdc",
    name: "channel-settings",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "general"));
      await page.getByRole("button", { name: "Channel settings" }).click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "direct-message",
    async take(studio) {
      return window(await open(studio, "neema", `/rooms/${studio.seeded.direct}`));
    },
  },
  {
    guide: "using-tdc",
    name: "thread",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "golang"));
      await page.getByRole("button", { name: "4 replies" }).first().click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "message-menu",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "general"));
      await page.getByText("Reminder: the October meetup").click({ button: "right" });
      await page.getByRole("menu").waitFor();
      await page.waitForTimeout(400);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "program-messages",
    async take(studio) {
      const page = await open(studio, "zawadi", room(studio, "nuxt"));
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "files",
    async take(studio) {
      const page = await open(studio, "juma", room(studio, "general"));
      await page.getByText("The poster for the 18th").scrollIntoViewIfNeeded();
      await page
        .getByRole("img", { name: /october-meetup/ })
        .first()
        .waitFor()
        .catch(() => {});
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "status",
    async take(studio) {
      const page = await open(studio, "neema", "/settings");
      return region(page, section(page, "Status"));
    },
  },
  {
    guide: "using-tdc",
    name: "privacy",
    async take(studio) {
      const page = await open(studio, "neema", "/settings");
      return region(page, section(page, "Privacy"));
    },
  },
  {
    guide: "using-tdc",
    name: "location",
    async take(studio) {
      const page = await open(studio, "neema", "/settings");
      return region(page, section(page, "Location"));
    },
  },
  {
    guide: "using-tdc",
    name: "notifications",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "announcements"));
      await page.getByRole("button", { name: /^Notifications/ }).click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "search",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "general"));
      await page
        .getByRole("button", { name: /^Search/ })
        .first()
        .click();
      await page.keyboard.type("meetup", { delay: 40 });
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "call",
    async take(studio) {
      const permissions = ["microphone", "camera"];
      const host = await open(studio, "neema", room(studio, "general"), { permissions });
      // By voice: a fake camera's test pattern says nothing about the call.
      await host.getByRole("button", { name: "Start a call" }).click();
      const guest = await open(studio, "juma", room(studio, "general"), { permissions });
      await guest.getByRole("button", { name: "Join", exact: true }).click();
      await host.waitForTimeout(4000);
      await settle(host);
      return window(host);
    },
  },
  {
    guide: "using-tdc",
    name: "call-presenting",
    async take(studio) {
      const host = await callInGeneral(studio);
      await expanded(host);
      // The fake capture device's test pattern stands in for a screen.
      await host.getByRole("button", { name: "Share your screen" }).click();
      await host.getByText("You are presenting").first().waitFor();
      await as(studio, "juma", "PUT", `/api/rooms/${studio.seeded.rooms.general}/call/hand`);
      await as(studio, "zawadi", "PUT", `/api/rooms/${studio.seeded.rooms.general}/call/hand`);
      await host.getByText("Hands up: 2").first().waitFor();
      await as(studio, "juma", "POST", `/api/rooms/${studio.seeded.rooms.general}/call/reactions`, {
        kind: "clap",
      });
      await host.waitForTimeout(600);
      return window(host);
    },
  },
  {
    guide: "using-tdc",
    name: "call-recording",
    async take(studio) {
      const host = await callInGeneral(studio);
      await host.getByRole("button", { name: "Record the call" }).click();
      await host.getByText("You are recording this call").first().waitFor();
      await settle(host);
      return region(host, host.locator("[data-call-window] [role=region]").first());
    },
  },
  {
    guide: "using-tdc",
    name: "call-links",
    async take(studio) {
      const host = await callInGeneral(studio);
      await as(studio, "neema", "POST", `/api/rooms/${studio.seeded.rooms.general}/call/links`, {
        label: "Speakers' green room",
        lobby: true,
        passcode: "",
        expiresInHours: 24,
        maxUses: 10,
      });
      await expanded(host);
      await host.getByRole("button", { name: "Links to this call" }).click();
      await host.getByLabel("Label (optional)").fill("Guests from DevFest Arusha");
      await host.getByText("Let people without an account join").click();
      await settle(host);
      return window(host);
    },
  },
  {
    guide: "using-tdc",
    name: "call-guest",
    async take(studio) {
      await callInGeneral(studio);
      const link = await as(
        studio,
        "neema",
        "POST",
        `/api/rooms/${studio.seeded.rooms.general}/call/links`,
        { label: "Guest speaker", lobby: true, guests: true, passcode: "", expiresInHours: 24 },
      );
      const url = String(link.url ?? `/call/${String(link.secret)}`);
      const page = await open(studio, null, new URL(url, studio.base).pathname, media);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "events",
    async take(studio) {
      return window(await open(studio, "juma", "/events"));
    },
  },
  {
    guide: "using-tdc",
    name: "events-month",
    async take(studio) {
      return window(await open(studio, "juma", "/events?view=month"));
    },
  },
  {
    guide: "using-tdc",
    name: "events-week",
    async take(studio) {
      const page = await open(studio, "juma", "/events?view=week");
      // On to the week the meetup is in, which may be the next.
      for (let step = 0; step < 2; step++) {
        if (await page.getByText("Dar Devs monthly meetup").count()) break;
        await page.getByRole("button", { name: "Next" }).click();
        await settle(page);
      }
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "event",
    async take(studio) {
      return window(await open(studio, "juma", `/events/${studio.seeded.event}`));
    },
  },
  {
    guide: "using-tdc",
    name: "automatic-replies",
    async take(studio) {
      const page = await open(studio, "rehema", "/settings");
      return region(page, page.locator("[data-automatic-replies]"));
    },
  },
  {
    guide: "using-tdc",
    name: "automatic-reply-mark",
    async take(studio) {
      const page = await open(studio, "rehema", room(studio, "general"));
      const mark = page.getByText("Automatic reply on").first();
      await mark.waitFor();
      const box = (await mark.boundingBox())!;
      return page.screenshot({ clip: { x: 0, y: box.y - 72, width: 240, height: 120 } });
    },
  },
  {
    guide: "using-tdc",
    name: "translation-settings",
    async take(studio) {
      const page = await open(studio, "juma", "/settings");
      return region(page, page.locator("#translation"));
    },
  },
  {
    guide: "using-tdc",
    name: "translation",
    async take(studio) {
      const page = await open(studio, "juma", room(studio, "general"));
      const message = page.locator(`[data-message-id="${studio.seeded.messages.swahili}"]`).first();
      await message.scrollIntoViewIfNeeded();
      await message.hover();
      await message.getByRole("button", { name: "Translate", exact: true }).click();
      await page.getByText("Welcome, Amani!").first().waitFor();
      await message.hover();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "api-tokens",
    async take(studio) {
      const page = await open(studio, "neema", "/settings");
      return region(page, page.locator("[data-api-tokens]"));
    },
  },
  {
    guide: "using-tdc",
    name: "scheduled",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "general"));
      await page
        .getByRole("button", { name: /Scheduled messages/ })
        .first()
        .click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "upload",
    async take(studio) {
      const page = await open(studio, "zawadi", room(studio, "nuxt"));
      // A slow connection, so the picture catches the upload part way.
      const cdp = await page.context().newCDPSession(page);
      await cdp.send("Network.emulateNetworkConditions", {
        offline: false,
        latency: 40,
        downloadThroughput: 4_000_000,
        uploadThroughput: 300_000,
      });
      await page.getByLabel("Choose files to attach").setInputFiles({
        name: "meetup-recording-raw.mp4",
        mimeType: "video/mp4",
        buffer: Buffer.alloc(24 * 1024 * 1024, 7),
      });
      await page.waitForTimeout(5000);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "transfer-offer",
    async take(studio) {
      const direct = `/rooms/${studio.seeded.direct}`;
      const receiver = await open(studio, "amani", direct);
      const sender = await open(studio, "neema", direct);
      await sender.getByRole("button", { name: "Attach", exact: true }).first().click();
      const chooser = sender.waitForEvent("filechooser");
      await sender.getByText("Send directly…").click();
      await (
        await chooser
      ).setFiles({
        name: "venue-walkthrough.mov",
        mimeType: "video/quicktime",
        buffer: Buffer.alloc(3 * 1024 * 1024, 1),
      });
      await receiver.getByRole("button", { name: "Accept" }).waitFor();
      await settle(receiver);
      return window(receiver);
    },
  },
  {
    guide: "using-tdc",
    name: "appearance",
    async take(studio) {
      const page = await open(studio, "neema", "/settings");
      return region(page, section(page, "Appearance"));
    },
  },
  {
    guide: "using-tdc",
    name: "dark",
    async take(studio) {
      return window(await open(studio, "neema", room(studio, "golang"), { dark: true }));
    },
  },
  {
    guide: "using-tdc",
    name: "shortcuts",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "general"));
      await page.keyboard.press("?");
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "using-tdc",
    name: "phone",
    async take(studio) {
      return window(await open(studio, "zawadi", room(studio, "nuxt"), { phone: true }));
    },
  },

  /* ------------------------------------------------------------ projects */
  {
    guide: "projects",
    name: "projects",
    async take(studio) {
      return window(await open(studio, "neema", "/projects"));
    },
  },
  {
    guide: "projects",
    name: "issues",
    async take(studio) {
      return window(await open(studio, "neema", "/projects/WEB"));
    },
  },
  {
    guide: "projects",
    name: "issue",
    async take(studio) {
      return window(await open(studio, "zawadi", "/issues/WEB-2"));
    },
  },
  {
    guide: "projects",
    name: "board",
    async take(studio) {
      const page = await open(studio, "neema", "/projects/WEB/board");
      // The sidebar away, so all four columns fit.
      await page.keyboard.press("[");
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "projects",
    name: "message-to-issue",
    async take(studio) {
      const page = await open(studio, "neema", room(studio, "general"));
      await page.getByText("Is there a list of who is speaking").click({ button: "right" });
      await page.getByRole("menuitem", { name: "Create an issue from this" }).click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "projects",
    name: "search",
    async take(studio) {
      const page = await open(studio, "neema", "/issues/search");
      await page.getByLabel("Words").fill("page");
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "projects",
    name: "views",
    async take(studio) {
      const page = await open(studio, "neema", "/projects/WEB");
      await page.getByRole("button", { name: "Views", exact: true }).click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "projects",
    name: "bulk",
    async take(studio) {
      const page = await open(studio, "neema", "/projects/WEB");
      const boxes = page.getByRole("checkbox");
      for (const index of [1, 3, 4]) await boxes.nth(index).check();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "projects",
    name: "cycles",
    async take(studio) {
      return window(await open(studio, "neema", "/projects/WEB/cycles"));
    },
  },
  {
    guide: "projects",
    name: "milestones",
    async take(studio) {
      return window(await open(studio, "neema", "/projects/WEB/milestones"));
    },
  },
  {
    guide: "projects",
    name: "progress",
    async take(studio) {
      return window(await open(studio, "neema", "/projects/WEB/progress"));
    },
  },
  {
    guide: "projects",
    name: "settings",
    async take(studio) {
      return window(await open(studio, "neema", "/projects/WEB/settings"));
    },
  },

  /* ----------------------------------------------------------- directory */
  {
    guide: "directory",
    name: "profile",
    async take(studio) {
      return window(await open(studio, "amani", "/directory/profile"));
    },
  },
  {
    guide: "directory",
    name: "search",
    async take(studio) {
      const page = await open(studio, "baraka", "/directory");
      await page.getByLabel("Skill").fill("Flutter");
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "directory",
    name: "person",
    async take(studio) {
      return window(await open(studio, "baraka", "/directory/people/amani"));
    },
  },
  {
    guide: "directory",
    name: "enquiries",
    async take(studio) {
      return window(await open(studio, "daudi", "/directory/enquiries"));
    },
  },
  {
    guide: "directory",
    name: "engagement",
    async take(studio) {
      const page = await open(
        studio,
        "amani",
        `/directory/engagements/${studio.seeded.engagement}`,
      );
      await page
        .getByRole("tab", { name: "Proposal" })
        .click()
        .catch(() => {});
      await settle(page);
      return window(page);
    },
  },
  {
    guide: "directory",
    name: "company",
    async take(studio) {
      return window(await open(studio, "zawadi", "/directory/companies/kilele-labs"));
    },
  },
  {
    guide: "directory",
    name: "review",
    async take(studio) {
      return window(await open(studio, "neema", "/directory/review"));
    },
  },

  /* ---------------------------------------- moving-from-telegram-and-whatsapp */
  {
    guide: "moving-from-telegram-and-whatsapp",
    name: "registration",
    async take(studio) {
      const page = await open(studio, "neema", "/admin");
      return region(page, section(page, "Who may join"));
    },
  },
  {
    guide: "moving-from-telegram-and-whatsapp",
    name: "invitations",
    async take(studio) {
      const page = await open(studio, "neema", "/people");
      return region(page, section(page, "Invitations"));
    },
  },
  {
    guide: "moving-from-telegram-and-whatsapp",
    name: "channels",
    async take(studio) {
      const page = await open(studio, "rehema", room(studio, "announcements"));
      return window(page);
    },
  },

  /* ------------------------------------------------------------- hosting */
  {
    guide: "hosting",
    name: "server-settings",
    async take(studio) {
      return window(await open(studio, "neema", "/admin"));
    },
  },
  {
    guide: "hosting",
    name: "configuration",
    async take(studio) {
      const page = await open(studio, "neema", "/admin");
      return region(page, section(page, "How this server was started"));
    },
  },
  {
    guide: "hosting",
    name: "federation",
    async take(studio) {
      const page = await open(studio, "neema", "/admin");
      return region(page, section(page, "Other servers"));
    },
  },
  {
    guide: "hosting",
    name: "backups",
    async take(studio) {
      const page = await open(studio, "neema", "/backups");
      return window(page);
    },
  },
  {
    guide: "hosting",
    name: "roles",
    async take(studio) {
      return window(await open(studio, "neema", "/roles"));
    },
  },
  {
    guide: "hosting",
    name: "audit",
    async take(studio) {
      return window(await open(studio, "neema", "/audit"));
    },
  },

  /* --------------------------------------------------------- integrating */
  {
    guide: "integrating",
    name: "installed-applications",
    async take(studio) {
      const page = await open(studio, "zawadi", "/integrations");
      return region(page, section(page, "Installed applications"));
    },
  },
  {
    guide: "integrating",
    name: "api-reference",
    async take(studio) {
      return window(await open(studio, "neema", "/help/api"));
    },
  },

  /* --------------------------------------------------- webhooks-and-apps */
  {
    guide: "webhooks-and-apps",
    name: "outgoing",
    async take(studio) {
      const page = await open(studio, "neema", "/integrations");
      return region(page, section(page, "Outgoing webhooks"));
    },
  },
  {
    guide: "webhooks-and-apps",
    name: "deliveries",
    async take(studio) {
      const page = await open(studio, "neema", "/integrations");
      const outgoing = section(page, "Outgoing webhooks");
      await outgoing.getByText("Delivery history").click();
      await settle(page);
      return region(page, outgoing.locator("li").filter({ hasText: "Meetup bot" }).first());
    },
  },
  {
    guide: "webhooks-and-apps",
    name: "incoming",
    async take(studio) {
      const page = await open(studio, "neema", "/integrations");
      const incoming = section(page, "Incoming webhooks");
      await incoming.getByRole("combobox").click();
      await page.getByRole("option", { name: /nuxt/ }).click();
      await settle(page);
      return region(page, incoming);
    },
  },
  {
    guide: "webhooks-and-apps",
    name: "applications",
    async take(studio) {
      return window(await open(studio, "neema", "/applications"));
    },
  },
];
