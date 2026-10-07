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
