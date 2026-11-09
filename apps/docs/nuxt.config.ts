import { fileURLToPath } from "node:url";

import { prepareGuide, remarkGuideLinks, siteCatalogue, type ParsedBody } from "./lib/guides";

/**
 * The public documentation site.
 *
 * One build per version: the dev, beta and main branches are each deployed
 * from their own branch with DOCS_VERSION naming it, and each deployment's
 * Studio commits to that same branch. Editing the development docs therefore
 * edits dev, and nothing reaches main except through the usual merge. The
 * header's switcher moves between the deployments; DOCS_VERSIONS says where
 * each one lives.
 *
 * DOCS_VERSION is read when the site is built, because the links a guide makes
 * into the repository are resolved then and point at that branch's files.
 */
const version = process.env.DOCS_VERSION || "dev";
const [owner = "AnoRebel", repo = "tdc"] = (process.env.DOCS_REPOSITORY || "AnoRebel/tdc").split(
  "/",
);

/** Where each version is published. NUXT_PUBLIC_DOCS_VERSIONS replaces it at runtime, as JSON. */
const versions = [
  { version: "main", label: "Stable", url: "https://tdc.anorebel.net" },
  { version: "beta", label: "Beta", url: "https://tdc-beta.anorebel.net" },
  { version: "dev", label: "Development", url: "https://tdc-dev.anorebel.net" },
];

const catalogue = siteCatalogue();

export default defineNuxtConfig({
  extends: ["docus"],
  modules: ["nuxt-studio"],

  compatibilityDate: "2026-10-01",

  // Page changes cross-fade (app/app.css); skipped for prefers-reduced-motion.
  experimental: { viewTransition: true },

  site: {
    name: "TDC",
    url: versions.find((entry) => entry.version === version)?.url,
    /*
     * Only the stable site is for search engines. The beta and development
     * sites describe work that may never ship, so they answer robots.txt with
     * "Disallow: /", mark every page noindex, and name the stable page as the
     * canonical one (app/composables/useDocsSeo.ts).
     */
    indexable: version === "main",
  },

  app: {
    head: {
      /*
       * The application's mark (apps/desktop/build/icons/tdc.svg), as files in
       * public/: an SVG for browsers that take one, an .ico for those that do
       * not, and a touch icon on a full-bleed ground because iOS rounds it.
       */
      link: [
        { rel: "icon", href: "/favicon.ico", sizes: "48x48" },
        { rel: "icon", href: "/favicon.svg", type: "image/svg+xml" },
        { rel: "apple-touch-icon", href: "/apple-touch-icon.png" },
        { rel: "manifest", href: "/site.webmanifest" },
      ],
      // The page ground in each scheme, so the browser's own chrome matches it.
      meta: [
        { name: "theme-color", content: "#ffffff", media: "(prefers-color-scheme: light)" },
        { name: "theme-color", content: "#18181b", media: "(prefers-color-scheme: dark)" },
      ],
    },
  },

  // The layer lists the guides in the sitemap from the content; the front page
  // is not content, so it is named here.
  sitemap: { urls: ["/"] },

  agentDiscovery: {
    // The module's "Content-Signal" line is not a directive search engines
    // know, and Lighthouse fails robots.txt over it. The AI crawlers it lists
    // are still allowed.
    robots: { aiPolicy: true, contentSignal: "" },
  },

  appConfig: {
    tdc: catalogue,
  },

  runtimeConfig: {
    /*
     * Analytics site ids, read from NUXT_UMAMI_SITE_ID and NUXT_RYBBIT_SITE_ID
     * when the server starts. Empty, as in development and on anybody else's
     * copy, means no script is added at all.
     */
    umamiSiteId: "",
    rybbitSiteId: "",
    public: {
      docs: {
        version,
        versions,
        repository: `${owner}/${repo}`,
      },
    },
  },

  content: {
    build: {
      markdown: {
        remarkPlugins: {
          "tdc-guide-links": { instance: remarkGuideLinks, options: { branch: version } },
        },
      },
    },
  },

  // The layer draws social preview images only while prerendering, which this
  // site does not do, and drawing them needs a native renderer in the image.
  ogImage: { enabled: false },

  studio: {
    // Editing on the filesystem during `nuxt dev` would write to this app's
    // content/ directory, which is not where the guides are. Locally, edit
    // docs/ in an editor; the site reloads.
    dev: false,
    repository: {
      provider: "github",
      owner,
      repo,
      branch: version,
      // Studio commits to <rootDir>/content/<file>; app/plugins/studio-paths
      // turns that into docs/<file>.
      rootDir: "apps/docs",
      private: true,
    },
    editor: {
      // The guides are read by the application as plain GitHub-flavoured
      // markdown. Components, styled spans and media are things it would print
      // as syntax, so the editor does not offer them.
      commands: { exclude: ["style", "insert", "image", "video"] },
    },
  },

  nitro: {
    /*
     * Bundled rather than traced into .output/server/node_modules. The
     * workspace holds two releases of unhead (the web app's and Nuxt UI's), and
     * the tracer copies one release's files under the other's version, so the
     * server fails at start looking for a file that release does not have.
     */
    externals: { inline: ["unhead", "@unhead/vue"] },
    /*
     * Rendered on request rather than prerendered, which the layer does by
     * default. A prerendered page carries the runtime configuration it was
     * built with, and here that configuration is what the container is given
     * when it starts: where the other versions live, and whether Studio is on.
     */
    prerender: { crawlLinks: false, ignore: ["/"] },
    publicAssets: [
      // The guides' screenshots, from docs/ where the guides name them.
      {
        dir: fileURLToPath(new URL("../../docs/images", import.meta.url)),
        baseURL: "/images",
        maxAge: 60 * 60 * 24,
      },
    ],
  },

  hooks: {
    "content:file:afterParse"(ctx) {
      if (ctx.collection.name !== "docs") return;
      prepareGuide(ctx.content.body as ParsedBody);
    },
  },
});
