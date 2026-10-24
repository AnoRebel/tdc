#!/usr/bin/env bun
/**
 * The guides' screenshots, made again from nothing.
 *
 *   bun run --cwd apps/web docs:screenshots
 *
 * Starts from an empty database, builds the server and the application, seeds
 * a small developer community (docs-seed.ts), photographs each screen a guide
 * describes, and writes them to docs/images/<guide>/<name>.webp with their
 * sizes in docs/images/sizes.json. Everything it started is stopped and the
 * database dropped afterwards, whether or not it succeeded.
 *
 * Run it after a change to the interface, look at what changed with
 * `git diff --stat docs/images`, and commit the images that should change.
 * Nothing in it is real: the people, the channels and the projects are made up
 * here, and the server it photographs exists only while it runs.
 *
 *   --skip-build     reuse the last server binary and application build
 *   --only=<guide>   photograph one guide's screens (projects, hosting, …),
 *                    or one screen (projects/board)
 *   --serve          seed, then leave the server running to look around in
 *                    (sessions are written to a file it names); Ctrl-C ends it
 *   --attach         photograph the server a --serve run left up, without
 *                    seeding again — for working on one screenshot
 *
 * TDC_DOCS_POSTGRES names the Postgres server (the compose file's by default),
 * TDC_DOCS_DATABASE the scratch database it creates and drops,
 * TDC_DOCS_PORT the port the server listens on (8131); the port after it takes
 * the seeded outgoing webhook's deliveries.
 */
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

import { SQL } from "bun";
import { chromium, type Browser, type BrowserContext, type Page } from "@playwright/test";

import { seed, type Seeded } from "./docs-seed";
import { shots, type Shot } from "./docs-shots";

const ROOT = join(import.meta.dirname, "..", "..", "..");
const IMAGES = join(ROOT, "docs", "images");
const POSTGRES = process.env.TDC_DOCS_POSTGRES ?? "postgres://tdc:tdc@127.0.0.1:55433";
const DATABASE = process.env.TDC_DOCS_DATABASE ?? "tdc_docs_screenshots";
const PORT = Number(process.env.TDC_DOCS_PORT ?? 8131);
const BASE = `http://127.0.0.1:${PORT}`;
const BINARY = join(tmpdir(), "tdc-docs-screenshots-server");

const flags = new Set(process.argv.slice(2));
const only = [...flags].find((flag) => flag.startsWith("--only="))?.slice("--only=".length);

/*
 * WebP at this quality keeps interface text crisp at a fifth of a PNG's size.
 * Encoded by the browser itself (a canvas), so there is no image library to
 * install for it.
 */
const QUALITY = 0.85;

function run(command: string, args: string[], cwd: string, env: Record<string, string> = {}) {
  console.log(`$ ${command} ${args.join(" ")}`);
  const result = spawnSync(command, args, {
    cwd,
    stdio: "inherit",
    env: { ...process.env, ...env },
  });
  if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed`);
}

async function withPostgres<T>(work: (sql: SQL) => Promise<T>): Promise<T> {
  const sql = new SQL(`${POSTGRES}/postgres?sslmode=disable`);
  try {
    return await work(sql);
  } finally {
    await sql.close();
  }
}

async function freshDatabase() {
  await withPostgres(async (sql) => {
    await sql.unsafe(`DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE)`);
    await sql.unsafe(`CREATE DATABASE ${DATABASE}`);
  });
}

async function dropDatabase() {
  await withPostgres((sql) => sql.unsafe(`DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE)`));
}

function build() {
  run("go", ["build", "-o", BINARY, "./cmd/tdc-server"], join(ROOT, "server"));
  // Empty, so the application calls the server it is served from.
  run("bun", ["run", "generate"], join(ROOT, "apps", "web"), { NUXT_PUBLIC_API_BASE: "" });
}

async function startServer(): Promise<ChildProcess> {
  const server = spawn(BINARY, [], {
    stdio: ["ignore", "ignore", "inherit"],
    env: {
      ...process.env,
      TDC_DATABASE_URL: `${POSTGRES}/${DATABASE}?sslmode=disable`,
      TDC_SERVER_WEB_ROOT: join(ROOT, "apps", "web", ".output", "public"),
      TDC_SERVER_ADDRESS: `127.0.0.1:${PORT}`,
      TDC_SERVER_SECURE_COOKIES: "false",
      // So people read as neema@community.example rather than neema@localhost.
      TDC_SERVER_CANONICAL_ADDRESS: "https://community.example",
      TDC_LOG_LEVEL: "error",
      TDC_TRANSLATION_LIBRETRANSLATE_URL: `http://127.0.0.1:${PORT + 1}`,
    },
  });
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      if ((await fetch(`${BASE}/api/health`)).ok) return server;
    } catch {
      // Not listening yet.
    }
    await Bun.sleep(200);
  }
  server.kill();
  throw new Error("the server did not start; is Postgres running?");
}

/*
 * LibreTranslate, as far as a screenshot needs it: the seeded Swahili
 * greeting comes back in English, and anything else as it was, said to be
 * English already.
 */
const TRANSLATIONS: Record<string, string> = {
  "Karibu sana Amani! Tutaonana kwenye meetup — nitaleta chai ya tangawizi kwa wote.":
    "Welcome, Amani! See you at the meetup — I'll bring ginger tea for everyone.",
};
async function translate(request: Request) {
  const { q } = (await request.json()) as { q: string[] };
  const translatedText = q.map((text) =>
    Object.entries(TRANSLATIONS).reduce((done, [from, to]) => done.replace(from, to), text),
  );
  const detectedLanguage = q.map((text) => ({
    language: Object.keys(TRANSLATIONS).some((from) => text.includes(from)) ? "sw" : "en",
    confidence: 90,
  }));
  return Response.json({ translatedText, detectedLanguage });
}

/** A PNG, as WebP, encoded by the page given; with its size. */
async function toWebp(encoder: Page, png: Buffer) {
  const { url, width, height } = await encoder.evaluate(
    async ({ data, quality }) => {
      const image = new Image();
      image.src = `data:image/png;base64,${data}`;
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = image.width;
      canvas.height = image.height;
      canvas.getContext("2d")!.drawImage(image, 0, 0);
      return {
        url: canvas.toDataURL("image/webp", quality),
        width: image.width,
        height: image.height,
      };
    },
    { data: png.toString("base64"), quality: QUALITY },
  );
  return { data: Buffer.from(url.slice(url.indexOf(",") + 1), "base64"), width, height };
}

/** Everything a shot is given: the seeded content and a way to sign in as anyone. */
export interface Studio {
  base: string;
  seeded: Seeded;
  browser: Browser;
  /** A browser context signed in as one of the seeded people, light theme, no tour. */
  as(
    username: string | null,
    options?: { phone?: boolean; dark?: boolean; permissions?: string[] },
  ): Promise<BrowserContext>;
}

async function photograph(seeded: Seeded) {
  const browser = await chromium.launch({
    args: [
      // A camera and microphone that exist, for the calls.
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
    ],
  });
  const encoder = await browser.newPage();
  const contexts: BrowserContext[] = [];

  const studio: Studio = {
    base: BASE,
    seeded,
    browser,
    async as(username, options = {}) {
      const context = await browser.newContext({
        baseURL: BASE,
        viewport: options.phone ? { width: 390, height: 844 } : { width: 1280, height: 800 },
        deviceScaleFactor: 1,
        isMobile: options.phone ?? false,
        hasTouch: options.phone ?? false,
        colorScheme: options.dark ? "dark" : "light",
        reducedMotion: "reduce",
        locale: "en-GB",
        timezoneId: "Africa/Dar_es_Salaam",
        permissions: options.permissions,
      });
      if (username) {
        await context.addCookies([
          { name: "tdc_session", value: seeded.people[username]!.token, url: BASE },
        ]);
      }
      contexts.push(context);
      return context;
    },
  };

  // A partial run keeps the sizes of the screenshots it did not take.
  const sizes: Record<string, [number, number]> =
    only && existsSync(join(IMAGES, "sizes.json"))
      ? (JSON.parse(readFileSync(join(IMAGES, "sizes.json"), "utf8")) as Record<
          string,
          [number, number]
        >)
      : {};
  const chosen = shots.filter(
    (shot: Shot) => !only || only === shot.guide || only === `${shot.guide}/${shot.name}`,
  );
  const failed: string[] = [];

  for (const shot of chosen) {
    const name = `${shot.guide}/${shot.name}.webp`;
    try {
      const png = await shot.take(studio);
      const { data, width, height } = await toWebp(encoder, png);
      mkdirSync(dirname(join(IMAGES, name)), { recursive: true });
      writeFileSync(join(IMAGES, name), data);
      sizes[name] = [width, height];
      console.log(`  ${name}  ${width}×${height}  ${(data.length / 1024).toFixed(0)} KB`);
    } catch (error) {
      failed.push(name);
      console.error(`  ${name}  FAILED: ${(error as Error).message.split("\n")[0]}`);
    } finally {
      // A shot's contexts are its own; the next starts clean.
      for (const context of contexts.splice(0)) await context.close();
    }
  }

  // One image to a line, as the formatter would write it.
  const lines = Object.entries(sizes)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, [width, height]]) => `  ${JSON.stringify(name)}: [${width}, ${height}]`);
  writeFileSync(join(IMAGES, "sizes.json"), `{\n${lines.join(",\n")}\n}\n`);
  await browser.close();
  return failed;
}

const SESSIONS = join(tmpdir(), "tdc-docs-screenshots-sessions.json");

function report(failed: string[]) {
  if (!failed.length) return;
  console.error(`\n${failed.length} screenshot(s) failed: ${failed.join(", ")}`);
  process.exitCode = 1;
}

async function main() {
  // Another run's --serve is still up: photograph what it seeded.
  if (flags.has("--attach")) {
    report(await photograph(JSON.parse(readFileSync(SESSIONS, "utf8")) as Seeded));
    return;
  }
  if (!flags.has("--skip-build")) build();
  await freshDatabase();
  let server: ChildProcess | undefined;
  // Where the seeded outgoing webhook delivers, which only has to say yes;
  // and a stand-in LibreTranslate for the translation screenshots.
  const receiver = Bun.serve({
    hostname: "127.0.0.1",
    port: PORT + 1,
    fetch: (request) =>
      new URL(request.url).pathname === "/translate"
        ? translate(request)
        : new Response(null, { status: 204 }),
  });
  try {
    server = await startServer();
    console.log("Seeding…");
    const seeded = await seed(BASE, `http://127.0.0.1:${PORT + 1}/hooks/golang`);
    if (flags.has("--serve")) {
      writeFileSync(SESSIONS, JSON.stringify(seeded, null, 2));
      console.log(`Serving ${BASE}; sessions in ${SESSIONS}. Ctrl-C stops and cleans up.`);
      await new Promise((resolve) => process.once("SIGINT", resolve));
      return;
    }
    console.log("Photographing…");
    report(await photograph(seeded));
  } finally {
    void receiver.stop(true);
    server?.kill();
    await dropDatabase();
  }
}

await main();
