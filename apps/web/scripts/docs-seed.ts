/**
 * The community the guides' screenshots show: a developer meetup group in Dar
 * es Salaam, its channels, a project with issues, and a few directory profiles.
 *
 * Made up, every word of it, and made through the API exactly as a person or a
 * program would make it — nothing is written to the database directly, so the
 * screenshots show what the server really produces. Used only by
 * docs-screenshots.ts, against a server that exists for the length of a run.
 */
import http from "node:http";

import { chromium } from "@playwright/test";

export const PASSWORD = "mvua-ya-vuli-2026";

interface Person {
  username: string;
  displayName: string;
  token: string;
  id: string;
}

export interface Seeded {
  people: Record<string, Person>;
  rooms: Record<string, string>;
  /** The direct conversation between Neema and Amani. */
  direct: string;
  /** Messages worth pointing at, by a short name. */
  messages: Record<string, string>;
  engagement: string;
  webhook: { id: string; url: string };
  application: string;
  /** The monthly meetup, as an event with a page of its own. */
  event: string;
}

/*
 * Neema runs the server and is who most screenshots are taken as. Amani and
 * Baraka are the developer and the client of the directory's walk-through in
 * using-tdc.md, so the pictures match the story the guide tells.
 */
const PEOPLE: [string, string][] = [
  ["neema", "Neema Mushi"],
  ["amani", "Amani Kimaro"],
  ["baraka", "Baraka Mwakyusa"],
  ["zawadi", "Zawadi Njeri"],
  ["juma", "Juma Hamisi"],
  ["rehema", "Rehema Ally"],
  ["daudi", "Daudi Ochieng"],
  ["halima", "Halima Said"],
  ["faraji", "Faraji Lyimo"],
  ["wanjiru", "Wanjiru Kamau"],
  ["tumaini", "Tumaini Massawe"],
];

// What the community's moderators may do, for the roles screenshot.
const MODERATING = ["message.delete.any", "moderation.restrict", "moderation.review"];

/*
 * Registration is limited to five an hour from one address, and sign-in to
 * thirty a quarter hour. Both are right, and a local server sees everybody as
 * 127.0.0.1 — so each person is registered and signed in from an address of
 * their own on the loopback network, which on Linux is all of 127.0.0.0/8.
 */
function fromAddress(
  base: string,
  index: number,
  path: string,
  body: unknown,
): Promise<{ status: number; body: unknown }> {
  const url = new URL(path, base);
  const data = JSON.stringify(body);
  return new Promise((resolve, reject) => {
    const request = http.request(
      {
        host: url.hostname,
        port: url.port,
        path: url.pathname,
        method: "POST",
        localAddress: `127.0.0.${10 + index}`,
        headers: { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) },
      },
      (response) => {
        let text = "";
        response.on("data", (chunk) => (text += chunk));
        response.on("end", () =>
          resolve({ status: response.statusCode ?? 0, body: text ? JSON.parse(text) : null }),
        );
      },
    );
    request.on("error", reject);
    request.end(data);
  });
}

/** A meetup poster, drawn by a browser, to send as an image. */
async function poster(): Promise<Buffer> {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 800, height: 500 } });
    await page.setContent(`
      <body style="margin:0;font-family:system-ui,sans-serif">
        <div style="height:500px;box-sizing:border-box;padding:56px;color:#fff;
          background:linear-gradient(135deg,#0f766e,#1e3a8a)">
          <div style="font-size:22px;letter-spacing:.2em;opacity:.8">DAR DEVELOPERS MEETUP</div>
          <div style="font-size:64px;font-weight:800;margin-top:24px;line-height:1.05">
            Go iterators &amp;<br>Nuxt on a 5 USD server</div>
          <div style="font-size:28px;margin-top:40px">Saturday 18 October · 10:00–14:00</div>
          <div style="font-size:24px;margin-top:8px;opacity:.85">Innovation hub, Kijitonyama · Karibuni wote</div>
        </div>
      </body>`);
    return await page.screenshot();
  } finally {
    await browser.close();
  }
}

type Api = <T = Record<string, unknown>>(
  who: string,
  method: string,
  path: string,
  body?: unknown,
) => Promise<T>;

/**
 * Seeds the server at base. receiver is an address that accepts outgoing
 * webhook deliveries.
 */
export async function seed(base: string, receiver: string): Promise<Seeded> {
  const people: Record<string, Person> = {};
  let sent = 0;

  const api: Api = async (who, method, path, body) => {
    const response = await fetch(new URL(path, base), {
      method,
      headers: {
        Authorization: `Bearer ${people[who]!.token}`,
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    if (!response.ok) throw new Error(`${who}: ${method} ${path} → ${response.status} ${text}`);
    return (text ? JSON.parse(text) : {}) as never;
  };

  /* ---------------------------------------------------------------- people */

  for (const [index, [username, displayName]] of PEOPLE.entries()) {
    const path = index === 0 ? "/api/setup" : "/api/auth/register";
    const made = await fromAddress(base, index, path, {
      username,
      password: PASSWORD,
      displayName,
      email: `${username}@example.org`,
    });
    if (made.status >= 300) throw new Error(`registering ${username}: ${JSON.stringify(made)}`);
    const signedIn = await fromAddress(base, index, "/api/auth/signin", {
      username,
      password: PASSWORD,
    });
    const session = signedIn.body as { token: string; user: { id: string } };
    if (!session?.token) throw new Error(`signing in ${username}: ${JSON.stringify(signedIn)}`);
    people[username] = { username, displayName, token: session.token, id: session.user.id };
  }

  for (const username of Object.keys(people)) {
    // The introduction is for a first arrival, not for a photograph.
    await api(username, "PUT", "/api/me/preferences/tours", { seen: ["chat"], reached: {} });
  }

  /* ---------------------------------------------------------------- server */

  for (const feature of ["directory", "calls", "events", "translation", "scheduled-messages"]) {
    await api("neema", "PUT", `/api/admin/features/${feature}`, { enabled: true });
  }
  const settings: Record<string, string> = {
    // The screenshots read the directory more often than a person does.
    "directory.reads_per_minute": "600",
    "directory.anonymous_reads_per_minute": "600",
    "directory.enquiries_per_hour": "50",
    "registration.policy": "invitation",
    "integrations.allow_private_addresses": "true",
    // For the calls' recording, link and guest screenshots.
    "calls.recording": "true",
    "calls.guests": "true",
  };
  for (const [key, value] of Object.entries(settings)) {
    await api("neema", "PUT", `/api/admin/settings/${key}`, { value });
  }

  /* --------------------------------------------------------------- rooms */

  const rooms: Record<string, string> = {};
  const channels: [string, string, string, string, string[]][] = [
    [
      "general",
      "General",
      "Karibu! Introductions, meetup news and anything that fits nowhere else.",
      "public",
      Object.keys(people),
    ],
    [
      "golang",
      "golang",
      "Go in production: services, tooling, and the monthly Go night.",
      "public",
      ["neema", "juma", "zawadi", "daudi", "amani", "faraji", "halima", "rehema"],
    ],
    [
      "nuxt",
      "nuxt",
      "Nuxt and Vue — SSR on small servers, i18n, and the community website.",
      "public",
      ["neema", "zawadi", "rehema", "wanjiru", "tumaini", "amani"],
    ],
    [
      "announcements",
      "Announcements",
      "Meetup dates and venue changes. Organisers post; everybody reads.",
      "public",
      Object.keys(people),
    ],
    [
      "jobs",
      "jobs",
      "Roles, contracts and gigs from members. Say where, and whether it is remote.",
      "public",
      ["neema", "baraka", "amani", "halima", "faraji"],
    ],
    [
      "organisers",
      "organisers",
      "Planning the next meetup.",
      "private",
      ["neema", "rehema", "daudi"],
    ],
  ];
  for (const [slug, name, topic, visibility, members] of channels) {
    const room = await api<{ id: string }>("neema", "POST", "/api/rooms", {
      slug,
      name,
      topic,
      visibility,
    });
    rooms[slug] = room.id;
    for (const member of members) {
      if (member === "neema") continue;
      if (visibility === "private") {
        const invitation = await api<{ token: string }>(
          "neema",
          "POST",
          `/api/rooms/${room.id}/invitations`,
          { maxUses: 1 },
        );
        await api(member, "POST", "/api/invitations/redeem", { token: invitation.token });
      } else {
        await api(member, "POST", `/api/rooms/${room.id}/join`);
      }
    }
  }

  const say = async (who: string, room: string, body: string) => {
    const message = await api<{ id: string }>(who, "POST", `/api/rooms/${room}/messages`, {
      body,
      clientToken: `docs-${++sent}`,
    });
    return message.id;
  };
  const reply = (who: string, room: string, root: string, body: string) =>
    api(who, "POST", `/api/rooms/${room}/threads/${root}/replies`, {
      body,
      clientToken: `docs-${++sent}`,
    });
  const react = (who: string, room: string, event: string, emoji: string) =>
    api(who, "PUT", `/api/rooms/${room}/messages/${event}/reactions`, { emoji });

  const messages: Record<string, string> = {};

  /*
   * An outgoing webhook on #golang, made before anything is said there so its
   * delivery history has something in it. The receiver is the screenshot
   * script's own, on this machine — hence the private-address setting.
   */
  const outgoing = await api<{ id: string }>("neema", "POST", "/api/me/webhooks", {
    roomId: rooms.golang,
    label: "Meetup bot",
    url: receiver,
    eventTypes: ["message.sent", "action.pressed"],
  });

  const sendFile = async (who: string, room: string, body: string, name: string, data: Buffer) => {
    const form = new FormData();
    form.append("file", new Blob([new Uint8Array(data)], { type: "image/png" }), name);
    const uploaded = await fetch(new URL(`/api/rooms/${room}/attachments`, base), {
      method: "POST",
      headers: { Authorization: `Bearer ${people[who]!.token}` },
      body: form,
    });
    if (!uploaded.ok)
      throw new Error(`uploading ${name}: ${uploaded.status} ${await uploaded.text()}`);
    const { id } = (await uploaded.json()) as { id: string };
    const message = await api<{ id: string }>(who, "POST", `/api/rooms/${room}/messages`, {
      body,
      attachmentIds: [id],
      clientToken: `docs-${++sent}`,
    });
    return message.id;
  };

  /* ------------------------------------------------------------- #general */

  const general = rooms.general!;
  await say(
    "neema",
    general,
    "Karibu everyone to the new server! This replaces the WhatsApp group — same people, but with channels, threads and search. Introduce yourselves here.",
  );
  await say(
    "zawadi",
    general,
    "Habari! Zawadi, frontend developer from Nairobi, mostly Vue and Nuxt these days. Here for the #nuxt channel and the October meetup.",
  );
  await say(
    "daudi",
    general,
    "Daudi from Mwanza. I look after infrastructure for a logistics startup — Go, Postgres, far too much Docker.",
  );
  messages.meetup = await say(
    "rehema",
    general,
    "Reminder: the October meetup is on Saturday the 18th at 10:00, at the innovation hub in Kijitonyama. Talks on Go iterators and Nuxt on a 5 USD server. Bring a laptop.",
  );
  for (const [who, emoji] of [
    ["juma", "🔥"],
    ["zawadi", "🙌"],
    ["daudi", "👍"],
    ["amani", "👍"],
    ["halima", "🎉"],
  ] as const) {
    await react(who, general, messages.meetup, emoji);
  }
  await reply(
    "juma",
    general,
    messages.meetup,
    "Will there be a livestream for those of us in Dodoma?",
  );
  await reply(
    "rehema",
    general,
    messages.meetup,
    "We'll record the talks and post them in #announcements the following week.",
  );
  await reply(
    "halima",
    general,
    messages.meetup,
    "Count me in. Can I bring two students from UDSM?",
  );
  await reply(
    "neema",
    general,
    messages.meetup,
    "Of course — the room takes sixty, we have about forty so far.",
  );
  await say(
    "amani",
    general,
    "Habari zenu! Amani from Arusha. Go and Flutter, mostly mobile money integrations. Happy to help anyone stuck on payment callbacks.",
  );
  messages.swahili = await say(
    "halima",
    general,
    "Karibu sana Amani! Tutaonana kwenye meetup — nitaleta chai ya tangawizi kwa wote.",
  );
  messages.poster = await sendFile(
    "rehema",
    general,
    "The poster for the 18th, for sharing in your other groups 👇",
    "october-meetup.png",
    await poster(),
  );
  await react("juma", general, messages.poster, "❤️");
  messages.welcome = await say(
    "tumaini",
    general,
    "Is there a list of who is speaking in November yet? I'd like to propose a talk on accessibility testing.",
  );

  /* --------------------------------------------------------------- #golang */

  const golang = rooms.golang!;
  messages.iterators = await say(
    "juma",
    golang,
    "Has anyone moved a service onto range-over-func iterators yet? Our M-Pesa callback handler has three nested loops I would love to flatten.",
  );
  await reply(
    "zawadi",
    golang,
    messages.iterators,
    "We did last month. slices.Collect and maps.Keys made the reconciliation job half the length. Happy to share the diff.",
  );
  await reply(
    "daudi",
    golang,
    messages.iterators,
    "Benchmark it first if it's on a hot path. We saw a small regression on the USSD gateway until we stopped allocating a closure per request.",
  );
  await reply(
    "juma",
    golang,
    messages.iterators,
    "Good to know — it handles about 40 requests a second at peak, so I'll measure before and after.",
  );
  await reply(
    "faraji",
    golang,
    messages.iterators,
    "Daudi, that sounds like a talk for the 18th 👀",
  );
  await react("neema", golang, messages.iterators, "👍");
  await react("amani", golang, messages.iterators, "💯");
  await say(
    "daudi",
    golang,
    "Fair warning for anyone on Go 1.26: the new GC default changed our container memory numbers. Set GOMEMLIMIT explicitly if you run in 512 MB.",
  );
  messages.pgx = await say(
    "halima",
    golang,
    "Question: pgx pool or database/sql for a new service? We have one Postgres, about 200 connections allowed, and four services sharing it.",
  );
  await say(
    "amani",
    golang,
    "pgxpool, and cap each service at 30. Leaves room for migrations and for you to connect with psql when something is on fire.",
  );
  await say(
    "neema",
    golang,
    "Daudi — would you give a 15-minute talk on that benchmark at the meetup? Rehema has a slot after the first break.",
  );
  messages.daudiTalk = await say(
    "daudi",
    golang,
    "Sure. I'll put the slides together this weekend and post them here first.",
  );
  await react("rehema", golang, messages.daudiTalk, "🙏");

  /* ----------------------------------------------------------------- #nuxt */

  const nuxt = rooms.nuxt!;
  messages.nuxtI18n = await say(
    "wanjiru",
    nuxt,
    "For the community site: are we doing Kiswahili with route prefixes (/sw/matukio) or a cookie? Prefixes are better for search, but double the routes.",
  );
  await say(
    "zawadi",
    nuxt,
    "Prefixes. Half our visitors arrive from a search on their phone, and a cookie means Google only ever sees the English pages.",
  );
  await say("rehema", nuxt, "Agreed. I'll file it on the project so it doesn't get lost — WEB-1.");
  messages.nuxtBuild = await say(
    "tumaini",
    nuxt,
    "The staging build is down to 210 KB of JavaScript after moving the map to a lazy component. Lighthouse on a mid-range Android is 92 now.",
  );
  await react("zawadi", nuxt, messages.nuxtBuild, "🚀");
  await react("wanjiru", nuxt, messages.nuxtBuild, "🚀");
  await react("neema", nuxt, messages.nuxtBuild, "👏");
  messages.rsvpBug = await say(
    "amani",
    nuxt,
    "The RSVP form rejects phone numbers written as +255 7xx — it only accepts 07xx. Can someone look before the 18th?",
  );

  /* ------------------------------------------------------- #announcements */

  await say(
    "neema",
    rooms.announcements!,
    "October meetup: Saturday 18th, 10:00–14:00, Kijitonyama. Talks: Go iterators in production (Daudi), Nuxt on a 5 USD server (Zawadi). Lunch is sponsored.",
  );

  /* ---------------------------------------------------------------- #jobs */

  await say(
    "baraka",
    rooms.jobs!,
    "Looking for a Flutter developer for a three-month contract: an ordering app for a chain of pharmacies in Dar and Morogoro, with mobile money checkout. Remote is fine.",
  );
  await say(
    "halima",
    rooms.jobs!,
    "Baraka, have a look at Amani's directory profile — that is exactly what he does.",
  );

  /* ----------------------------------------------------------- #organisers */

  await say(
    "rehema",
    rooms.organisers!,
    "Venue confirmed for the 18th. They need the attendee list by Thursday.",
  );
  await say("daudi", rooms.organisers!, "I'll bring the projector adapter this time 😅");

  /* ------------------------------------------------------------- directs */

  const direct = (
    await api<{ id: string }>("neema", "POST", "/api/direct", { userIds: [people.amani!.id] })
  ).id;
  await say(
    "amani",
    direct,
    "Hi Neema, could I get the #jobs channel pinned to the top of the sidebar for new members?",
  );
  await say(
    "neema",
    direct,
    "Pinning is per person for now, but I'll put a link to it in #general's topic.",
  );
  await say("amani", direct, "Perfect, asante sana.");
  await say(
    "neema",
    direct,
    "Also — would you review Halima's question on pgx in #golang? You answered it better than I could.",
  );
  await api("zawadi", "POST", "/api/direct", { userIds: [people.neema!.id] }).then((room) =>
    say(
      "zawadi",
      (room as { id: string }).id,
      "Slides for the Nuxt talk are nearly done. Ten minutes or fifteen?",
    ),
  );

  /* -------------------------------------------------------------- statuses */

  await api("daudi", "PUT", "/api/me/status", { status: "busy", text: "Deploying until 15:00" });
  await api("zawadi", "PUT", "/api/me/status", { status: "away", text: "Travelling to Dar" });
  await api("neema", "PUT", "/api/me/status", { status: "available", text: "" });
  await api("neema", "PUT", "/api/me/location", {
    mode: "manual",
    place: "Dar es Salaam, Tanzania",
    visibility: "members",
  });

  /* ------------------------------------------------------- incoming webhook */

  const hook = await api<{ id: string; url?: string; token?: string }>(
    "neema",
    "POST",
    `/api/rooms/${nuxt}/incoming-webhooks`,
    { label: "Build server" },
  );
  const hookUrl = new URL(hook.url ?? `/api/hooks/${hook.token}`, base).href;
  // The address names the server's public host; this one is local.
  await fetch(new URL(new URL(hookUrl).pathname, base), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      text: "Build 214 of community-site passed on main in 2m 41s and was deployed to staging.",
      actions: [
        {
          name: "log",
          label: "See the build",
          capability: "open",
          url: "https://ci.example.org/community-site/214",
        },
      ],
    }),
  }).then((response) => {
    if (!response.ok) throw new Error(`posting through the webhook: ${response.status}`);
  });

  /* ---------------------------------------------------------- application */

  const application = await api<{ id: string }>("neema", "POST", "/api/admin/applications", {
    name: "Deploy bot",
    purpose: "Asks before deploying the community website to production, and posts the result.",
    permissions: ["channel.post"],
    sendsDataExternally: true,
    externalDisclosure: "Build numbers and who pressed Deploy, to ci.example.org",
  });
  const { credential } = await api<{ credential: { token: string } }>(
    "neema",
    "POST",
    `/api/admin/applications/${application.id}/approve`,
    { permissions: ["channel.post"], channels: [nuxt] },
  );
  const bot = await fetch(new URL(`/api/rooms/${nuxt}/messages`, base), {
    method: "POST",
    headers: { Authorization: `Bearer ${credential.token}`, "Content-Type": "application/json" },
    body: JSON.stringify({
      body: "Release 2.3.0 of the community website is ready. Deploy it to production?",
      clientToken: "docs-deploy",
      actions: [
        { name: "deploy", label: "Deploy", once: true },
        { name: "hold", label: "Not yet" },
        {
          name: "log",
          label: "See the changes",
          capability: "open",
          url: "https://ci.example.org/community-site/releases/2.3.0",
        },
      ],
    }),
  });
  if (!bot.ok) throw new Error(`the bot's message: ${bot.status} ${await bot.text()}`);
  messages.deploy = ((await bot.json()) as { id: string }).id;
  await say("zawadi", nuxt, "Once Amani's RSVP fix is in, let's press it.");

  /* -------------------------------------------------------- outgoing hooks */

  /* --------------------------------------------------------------- tokens */

  await api("neema", "POST", "/api/me/tokens", {
    label: "Meetup calendar sync",
    scope: ["channel.post", "channel.read"],
  });
  await api("neema", "POST", "/api/me/tokens", { label: "Backup script", scope: ["channel.read"] });

  /* ----------------------------------------------------------- invitations */

  await api("neema", "POST", "/api/admin/invitations", {
    maxUses: 40,
    expiresInHours: 24 * 14,
    withCode: true,
  });
  await api("neema", "POST", "/api/admin/invitations", {
    boundTo: "imani@example.org",
    maxUses: 1,
    expiresInHours: 72,
  });

  /* ---------------------------------------------------------------- events */

  const event = await seedEvents(api, rooms, (name) => people[name]!.id);

  /* ---------------------------------------------------- automatic replies */

  // Rehema is at a conference: whoever writes is told who to ask instead.
  await api("rehema", "POST", "/api/me/automatic-replies", {
    enabled: true,
    message:
      "I'm at DevFest Arusha until Monday with patchy signal. For anything about the venue, ask @daudi.",
    mentionMessage: "Away until Monday — @daudi has the venue details.",
    statuses: [],
    timeZone: "Africa/Dar_es_Salaam",
    replyToDirect: true,
    replyToGroups: true,
    replyToMentions: true,
  });

  // Juma reads Swahili through the server's LibreTranslate, which the
  // screenshots stand in for: a browser's own translation is not in a
  // headless one.
  await api("juma", "PUT", "/api/me/translation/preferences", {
    provider: "libretranslate",
    targetLanguage: "",
    autoTranslate: false,
  });

  /* ----------------------------------------------------- scheduled messages */

  const later = (hours: number) => new Date(Date.now() + hours * 3_600_000).toISOString();
  await api("neema", "POST", `/api/rooms/${rooms.announcements}/scheduled`, {
    body: "Doors open at 17:30 on meetup day — bring your ID for the building's front desk.",
    sendAt: later(20),
  });
  await api("neema", "POST", `/api/rooms/${rooms.general}/scheduled`, {
    body: "Slides from the talks are in #announcements. Asante sana kwa kuja!",
    sendAt: later(30),
  });

  /* ------------------------------------------------------------------ roles */

  const moderators = await api<{ id: string }>("neema", "POST", "/api/admin/roles", {
    name: "Moderators",
    description: "Keep the channels friendly: remove messages, restrict people, review reports.",
  });
  for (const permission of MODERATING) {
    await api("neema", "PUT", `/api/admin/roles/${moderators.id}/permissions`, {
      permission,
      granted: true,
    });
  }
  const hosts = await api<{ id: string }>("neema", "POST", "/api/admin/roles", {
    name: "Event hosts",
    description: "Run the meetups: make call links and record the talks.",
  });
  for (const permission of ["call.link", "call.record"]) {
    await api("neema", "PUT", `/api/admin/roles/${hosts.id}/permissions`, {
      permission,
      granted: true,
    });
  }
  for (const [role, member] of [
    [moderators.id, "daudi"],
    [moderators.id, "rehema"],
    [hosts.id, "rehema"],
    [hosts.id, "zawadi"],
  ] as const) {
    await api("neema", "POST", `/api/admin/roles/${role}/members`, {
      user: people[member]!.id,
    });
  }

  /* --------------------------------------------------------------- backups */

  await api("neema", "POST", "/api/admin/backups", {
    parts: ["settings", "accounts", "channels", "projects"],
    passphrase: "pole-pole-ndio-mwendo",
  });

  /* -------------------------------------------------------------- projects */

  await seedProjects(api, rooms, messages);

  /* ------------------------------------------------------------- directory */

  const engagement = await seedDirectory(api);

  return {
    people,
    rooms,
    direct,
    messages,
    engagement,
    webhook: { id: outgoing.id, url: hookUrl },
    application: application.id,
    event,
  };
}

/*
 * The group's coming events, dated from today so the screenshots always show
 * them ahead: the monthly meetup with its talks, a Go night, a workshop and
 * office hours.
 */
async function seedEvents(api: Api, rooms: Record<string, string>, id: (name: string) => string) {
  // A time in Dar es Salaam (UTC+3), some days from now.
  const at = (days: number, hour: number, minute = 0) => {
    const day = new Date(Date.now() + days * 86_400_000);
    return new Date(
      Date.UTC(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate(), hour - 3, minute),
    ).toISOString();
  };
  const plain = {
    timeZone: "Africa/Dar_es_Salaam",
    allDay: false,
    recurrence: "",
    exceptions: [],
    online: false,
    onlineUrl: "",
    location: "",
    capacity: null,
    visibility: "public",
  };
  const meetup = await api<{ id: string }>("neema", "POST", "/api/events", {
    ...plain,
    channelId: rooms.general,
    kind: "meetup",
    title: "Dar Devs monthly meetup",
    description:
      "Two talks, then food and talk. **Venue:** Buni Hub, COSTECH, Kijitonyama. The room takes sixty.",
    startsAt: at(4, 17, 30),
    endsAt: at(4, 20, 30),
    location: "Buni Hub, COSTECH, Kijitonyama",
    capacity: 60,
    hosts: [id("rehema")],
    sessions: [
      {
        title: "Range-over-func iterators in a payments service",
        startsAt: at(4, 18),
        endsAt: at(4, 18, 40),
        speakers: [id("juma")],
      },
      {
        title: "SSR on a 1 GB server: what we learned",
        startsAt: at(4, 18, 50),
        endsAt: at(4, 19, 30),
        speakers: [id("zawadi")],
      },
    ],
  });
  for (const [who, status] of [
    ["juma", "going"],
    ["zawadi", "going"],
    ["halima", "going"],
    ["faraji", "maybe"],
    ["amani", "going"],
    ["tumaini", "going"],
  ] as const) {
    await api(who, "PUT", `/api/events/${meetup.id}/rsvp`, { status });
  }
  await api("daudi", "POST", "/api/events", {
    ...plain,
    channelId: rooms.golang,
    kind: "meetup",
    title: "Go night: profiling with pprof",
    description: "Bring a laptop and a slow endpoint.",
    startsAt: at(9, 18),
    endsAt: at(9, 20),
    location: "Kilele Labs, Masaki",
  });
  await api("zawadi", "POST", "/api/events", {
    ...plain,
    channelId: rooms.nuxt,
    kind: "workshop",
    title: "Workshop: i18n for the community website",
    description: "Hands-on: Swahili routes and hreflang. Laptops with Node 22.",
    startsAt: at(12, 10),
    endsAt: at(12, 13),
    online: true,
    capacity: 20,
  });
  await api("neema", "POST", "/api/events", {
    ...plain,
    kind: "office_hours",
    title: "Organisers' office hours",
    description: "Ask us anything about speaking, sponsoring or the code of conduct.",
    startsAt: at(2, 12),
    endsAt: at(2, 13),
    online: true,
    recurrence: "FREQ=WEEKLY;COUNT=6",
  });
  return meetup.id;
}

async function seedProjects(
  api: Api,
  rooms: Record<string, string>,
  messages: Record<string, string>,
) {
  const project = await api<{
    states: { id: string; name: string }[];
    priorities: { id: string; name: string }[];
  }>("neema", "POST", "/api/projects", {
    key: "WEB",
    name: "Community website",
    description: "The meetup group's site: events, speakers, RSVPs and the code of conduct.",
    visibility: "public",
    states: [
      { name: "To do", isCompletion: false },
      { name: "In progress", isCompletion: false },
      { name: "In review", isCompletion: false },
      { name: "Done", isCompletion: true },
    ],
  });
  const state = (name: string) => project.states.find((s) => s.name === name)!.id;

  for (const member of ["rehema", "zawadi", "amani", "wanjiru", "tumaini", "daudi"]) {
    await api("neema", "PUT", `/api/projects/WEB/members/${member}`, { role: "member" });
  }

  const labels: [string, string][] = [
    ["bug", "#dc2626"],
    ["feature", "#2563eb"],
    ["i18n", "#7c3aed"],
    ["infrastructure", "#0d9488"],
    ["docs", "#ca8a04"],
  ];
  const labelIds: Record<string, string> = {};
  for (const [name, colour] of labels) {
    const made = await api<{ id: string }>("neema", "POST", "/api/projects/WEB/labels", {
      name,
      colour,
    });
    labelIds[name] = made.id;
  }
  // Urgent, High, Medium, Low.
  const priority = (index: number) => project.priorities[index]?.id;

  await api("neema", "PUT", "/api/projects/WEB/planning", {
    timeZone: "Africa/Dar_es_Salaam",
    cyclePolicy: "carry-forward",
  });
  const today = new Date();
  const day = (offset: number) =>
    new Date(today.getTime() + offset * 86_400_000).toISOString().slice(0, 10);
  const cycle = await api<{ id: string }>("neema", "POST", "/api/projects/WEB/cycles", {
    name: "October sprint",
    startsOn: day(-6),
    endsOn: day(8),
  });
  await api("neema", "POST", "/api/projects/WEB/cycles", {
    name: "November sprint",
    startsOn: day(9),
    endsOn: day(23),
  });
  const milestone = await api<{ id: string }>("neema", "POST", "/api/projects/WEB/milestones", {
    name: "New site launch",
    description: "Everything the site needs before the November meetup is announced on it.",
    targetDate: day(30),
  });
  const later = await api<{ id: string }>("neema", "POST", "/api/projects/WEB/milestones", {
    name: "Photos and speakers",
    description: "Event photos served from object storage, and a page for every speaker.",
    targetDate: day(75),
  });

  type Issue = {
    title: string;
    description: string;
    by: string;
    state: string;
    labels: string[];
    assignee?: string;
    priority?: number;
    origin?: [string, string];
  };
  const issues: Issue[] = [
    {
      title: "Kiswahili translations for the event pages",
      description:
        "Serve /sw/matukio alongside /events, with hreflang links, as agreed in #nuxt. Start with the event list and the RSVP form.",
      by: "rehema",
      state: "In progress",
      labels: ["i18n", "feature"],
      assignee: "wanjiru",
      priority: 1,
    },
    {
      title: "RSVP form rejects +255 phone numbers",
      description:
        "Numbers written as +255 7xx xxx xxx are refused; only 07xx is accepted. Normalise to E.164 before validating.",
      by: "amani",
      state: "In review",
      labels: ["bug"],
      assignee: "amani",
      priority: 0,
      origin: [rooms.nuxt!, messages.rsvpBug!],
    },
    {
      title: "Speaker profile pages",
      description: "One page per speaker with their talks, links and a photo they choose.",
      by: "zawadi",
      state: "To do",
      labels: ["feature"],
      assignee: "zawadi",
      priority: 2,
    },
    {
      title: "Move event photos off the shared VPS",
      description:
        "Photos are 4 GB and growing. Move them to object storage and serve resized copies.",
      by: "daudi",
      state: "To do",
      labels: ["infrastructure"],
      assignee: "daudi",
      priority: 2,
    },
    {
      title: "Publish the code of conduct",
      description: "Adapted from the draft in #organisers, in English and Kiswahili.",
      by: "neema",
      state: "Done",
      labels: ["docs", "i18n"],
      assignee: "neema",
      priority: 1,
    },
    {
      title: "Calendar export (.ics) for meetups",
      description: "A link on each event that adds it to a phone's calendar.",
      by: "tumaini",
      state: "To do",
      labels: ["feature"],
      priority: 3,
    },
    {
      title: "Sponsors section on the home page",
      description: "Logos and a line each, with a link. Sponsors approve their own text.",
      by: "rehema",
      state: "In progress",
      labels: ["feature"],
      assignee: "tumaini",
      priority: 2,
    },
    {
      title: "Lighthouse score below 80 on the schedule page",
      description: "The map component loads on every page. Make it lazy.",
      by: "tumaini",
      state: "Done",
      labels: ["bug"],
      assignee: "tumaini",
      priority: 1,
    },
    {
      title: "Dark mode for the schedule page",
      description: "Follow the system setting; the projector at the hub washes out light pages.",
      by: "zawadi",
      state: "To do",
      labels: ["feature"],
      priority: 3,
    },
  ];

  for (const [index, issue] of issues.entries()) {
    const made = await api<{ key: string }>(issue.by, "POST", "/api/projects/WEB/issues", {
      title: issue.title,
      description: issue.description,
      stateId: state("To do"),
      ...(issue.origin ? { originChannel: issue.origin[0], originEvent: issue.origin[1] } : {}),
    });
    const ref = made.key;
    for (const label of issue.labels) {
      await api("neema", "PUT", `/api/issues/${ref}/labels/${labelIds[label]}`);
    }
    if (issue.assignee) await api("neema", "PUT", `/api/issues/${ref}/assignees/${issue.assignee}`);
    const chosen = issue.priority === undefined ? undefined : priority(issue.priority);
    if (chosen) await api("neema", "PUT", `/api/issues/${ref}/priority`, { priorityId: chosen });
    if (index < 7) await api("neema", "PUT", `/api/issues/${ref}/cycle`, { cycleId: cycle.id });
    const aim = index === 2 || index === 3 ? later : index === 8 ? undefined : milestone;
    if (aim) await api("neema", "PUT", `/api/issues/${ref}/milestone`, { milestoneId: aim.id });
    if (issue.state !== "To do") {
      if (issue.state === "Done" || issue.state === "In review") {
        await api(issue.assignee ?? "neema", "POST", `/api/issues/${ref}/state`, {
          stateId: state("In progress"),
        });
      }
      await api(issue.assignee ?? "neema", "POST", `/api/issues/${ref}/state`, {
        stateId: state(issue.state),
      });
    }
  }
  await api("neema", "POST", "/api/issues/WEB-3/relations", { kind: "blocked_by", issue: "WEB-4" });

  // The issue's discussion is a room whose id is the issue's.
  const rsvp = await api<{ id: string }>("neema", "GET", "/api/issues/WEB-2");
  await api("zawadi", "POST", `/api/rooms/${rsvp.id}/join`);
  await api("amani", "POST", `/api/rooms/${rsvp.id}/messages`, {
    body: "Fix is up: numbers are normalised to +255 before validation, and 07xx still works. Zawadi, could you review?",
    clientToken: "docs-rsvp-1",
  });
  await api("zawadi", "POST", `/api/rooms/${rsvp.id}/messages`, {
    body: "Looks good. Tried +255 754 123 456, 0754123456 and 255754123456 — all accepted.",
    clientToken: "docs-rsvp-2",
  });

  await api("neema", "POST", "/api/projects/WEB/views", {
    name: "Bugs before the launch",
    layout: "list",
    filter: { label: "bug", sort: "priority" },
    shared: true,
  });
  await api("neema", "POST", "/api/projects/WEB/views", {
    name: "This sprint, by state",
    layout: "board",
    filter: { cycle: "current" },
    shared: true,
  });

  // A second project, so the list is a list.
  const api2 = await api<{ states: { id: string; name: string }[] }>(
    "daudi",
    "POST",
    "/api/projects",
    {
      key: "OPS",
      name: "Meetup infrastructure",
      description: "The server this community runs on, its backups, and the livestream kit.",
      visibility: "public",
    },
  );
  for (const title of [
    "Nightly database backup to a second region",
    "Upgrade Postgres 16 to 18",
    "Test the livestream kit before the 18th",
  ]) {
    await api("daudi", "POST", "/api/projects/OPS/issues", {
      title,
      description: "",
      stateId: api2.states[0]!.id,
    });
  }
  await api("neema", "POST", "/api/projects", {
    key: "TALKS",
    name: "Talk proposals",
    description: "Proposals for the monthly meetups, reviewed by the organisers.",
    visibility: "private",
  });
}

async function seedDirectory(api: Api): Promise<string> {
  const profiles: Record<string, Record<string, unknown>> = {
    amani: {
      headline: "Go and Flutter developer — mobile money integrations",
      bio: "Seven years building payment flows for East African businesses: M-Pesa, Airtel Money and Mixx by Yas callbacks, reconciliation jobs, and the Flutter apps in front of them. I like boring, well-tested code and clear invoices.",
      location: "Arusha, Tanzania",
      experience: "7 years",
      skills: ["Go", "Flutter", "Mobile money integration", "PostgreSQL"],
      openToWork: true,
      availabilityNote: "Two days a week from November",
      contactEmail: "amani@example.org",
    },
    zawadi: {
      headline: "Frontend developer — Vue, Nuxt and accessible interfaces",
      bio: "I build fast, accessible web apps for people on mid-range Android phones and patchy connections. Currently leading the frontend of a logistics dashboard in Nairobi.",
      location: "Nairobi, Kenya",
      experience: "5 years",
      skills: ["Vue", "Nuxt", "TypeScript", "UI design"],
      openToWork: false,
    },
    daudi: {
      headline: "Infrastructure and DevOps — Go, Postgres, Kubernetes",
      bio: "I keep services running on small budgets: right-sized VPSes, Postgres tuning, and deployments a team of three can understand.",
      location: "Mwanza, Tanzania",
      experience: "9 years",
      skills: ["Go", "Kubernetes", "PostgreSQL", "Linux administration"],
      openToWork: true,
      availabilityNote: "Short engagements only",
    },
    wanjiru: {
      headline: "Product designer — research-led, bilingual interfaces",
      bio: "UX research and interface design for products used in English and Kiswahili.",
      location: "Nairobi, Kenya",
      experience: "6 years",
      skills: ["UI design", "UX research", "Product management"],
      openToWork: true,
    },
    halima: {
      headline: "Backend engineer — Django and data pipelines",
      bio: "Python services and the reports that come out of them, for health and agriculture NGOs.",
      location: "Zanzibar, Tanzania",
      experience: "4 years",
      skills: ["Python", "Django", "PostgreSQL", "Data analysis"],
      openToWork: true,
    },
    tumaini: {
      headline: "Web performance and accessibility",
      bio: "Making sites work on the phones people actually have.",
      location: "Moshi, Tanzania",
      experience: "3 years",
      skills: ["JavaScript", "Vue", "React"],
      openToWork: false,
    },
  };
  for (const [username, profile] of Object.entries(profiles)) {
    const { skills, location, ...rest } = profile as {
      skills: string[];
      location: string;
      openToWork: boolean;
    };
    // A location is the account's, which the profile shows.
    await api(username, "PUT", "/api/me/location", {
      mode: "manual",
      place: location,
      visibility: "members",
    });
    await api(username, "PUT", "/api/directory/me", {
      ...rest,
      visibility: "public",
      detailsVisibility: "members",
      contactVisibility: "hidden",
      skills: skills.map((name) => ({ name, description: "" })),
      // Open to work has to say until when, at most half a year ahead.
      availableUntil: rest.openToWork
        ? new Date(Date.now() + 120 * 86_400_000).toISOString()
        : undefined,
      portfolio: [],
      acceptsEnquiries: true,
    });
  }

  // A skill waiting for review, for the moderation screenshot.
  await api("amani", "POST", "/api/directory/skills", {
    name: "Supabase",
    description: "Hosted Postgres with auth and storage",
  }).catch(() => {});
  await api("halima", "POST", "/api/directory/skills", {
    name: "Apache Airflow",
    description: "Scheduling data pipelines",
  }).catch(() => {});

  await api("zawadi", "POST", "/api/directory/companies", {
    slug: "kilele-labs",
    name: "Kilele Labs",
    description: "A small studio building web and mobile products for East African businesses.",
    location: "Nairobi and Dar es Salaam",
    visibility: "public",
  });
  for (const member of ["amani", "wanjiru"]) {
    await api("zawadi", "POST", "/api/directory/companies/kilele-labs/members", {
      username: member,
      role: "Contractor",
    });
  }
  await api("amani", "POST", "/api/directory/me/companies/kilele-labs/accept");

  // The walk-through in using-tdc.md: Baraka asks, Amani takes it up.
  const enquiry = await api<{ id: string }>(
    "baraka",
    "POST",
    "/api/directory/profiles/amani/enquiries",
    {
      subject: "Pharmacy ordering app with mobile money checkout",
      body: "We run eleven pharmacies in Dar es Salaam and Morogoro and want customers to order and pay from their phones. Flutter for Android first, with M-Pesa and Airtel Money. We would like to start in November and launch by February.",
    },
  );
  const accepted = await api<{ id?: string; engagement?: { id: string }; engagementId?: string }>(
    "amani",
    "POST",
    `/api/directory/enquiries/${enquiry.id}/accept`,
  );
  const engagement = accepted.engagementId ?? accepted.engagement?.id ?? accepted.id!;
  await api("amani", "POST", `/api/directory/engagements/${engagement}/proposals`, {
    scope:
      "An Android app in Flutter for browsing stock, ordering and paying, with an order dashboard for the pharmacies.",
    timeline:
      "November to the end of January, with a test release to two pharmacies in mid-December.",
    currency: "TZS",
    lineItems: [
      { description: "Ordering app (Flutter, Android)", amountMinor: 950_000_000 },
      { description: "M-Pesa and Airtel Money checkout", amountMinor: 420_000_000 },
      { description: "Order dashboard for pharmacy staff", amountMinor: 380_000_000 },
    ],
  });
  const negotiation = await api<{ versions: { id: string }[] }>(
    "baraka",
    "GET",
    `/api/directory/engagements/${engagement}/proposals`,
  );
  await api(
    "baraka",
    "POST",
    `/api/directory/engagements/${engagement}/proposals/${negotiation.versions[0]!.id}/responses`,
    {
      kind: "request_changes",
      part: "timeline",
      comment: "Could the test release be at the start of December? Our busiest month is January.",
    },
  ).catch(() => {});

  // A second enquiry, still waiting, for the enquiries list.
  await api("baraka", "POST", "/api/directory/profiles/daudi/enquiries", {
    subject: "Moving our ordering system off a single server",
    body: "Our backend runs on one VPS and went down twice last month. Could you look at it and propose something we can afford?",
  });
  return engagement;
}
