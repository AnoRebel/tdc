import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";

import { PREPAINT_THEME_SCRIPT } from "./app/lib/theme";

export default defineNuxtConfig({
  compatibilityDate: "2026-08-29",

  future: {
    // Opt into Nuxt 5 behaviour ahead of its release, which is the supported
    // way to avoid a large migration later.
    compatibilityVersion: 5,
  },

  // The app is a client of a remote TDC server, not a renderer of its own
  // content: there is nothing to render on the server that is not behind
  // authentication, and a single-page app is what the desktop client embeds.
  ssr: false,

  app: {
    head: {
      // The title template lives in app.vue: a function here does not survive
      // serialisation into the built config and is silently dropped.
      htmlAttrs: { lang: "en" },
      meta: [
        {
          name: "description",
          content:
            "Chat, project tracking, and a directory connecting developers " +
            "with the people who need them.",
        },
        /*
         * The browser is told which themes exist, so its own chrome — the
         * scrollbars, the form controls it draws itself — matches rather than
         * rendering a light scrollbar beside a dark page.
         */
        { name: "color-scheme", content: "dark light" },
      ],
      script: [
        {
          /*
           * The theme, decided before the first paint.
           *
           * In the <head> and inline, so it runs before anything is drawn: a
           * class applied from a component runs after the browser has already
           * painted, and the flash of the wrong theme is exactly as long as
           * that takes.
           *
           * The palette treats dark as the default — its rule is
           * `.dark, :root:not(.light)` — so what this sets is `.light` for
           * somebody who wants light. Both classes are set explicitly anyway,
           * because a stylesheet that changes which way it defaults should not
           * silently change what everybody sees.
           *
           * Wrapped in try/catch: localStorage throws in a private window in
           * some browsers, and a theme preference is not worth a blank page.
           */
          key: "theme-prepaint",
          innerHTML: PREPAINT_THEME_SCRIPT,
        },
      ],
    },
  },

  devtools: { enabled: true },

  modules: ["@vueuse/nuxt", "@vite-pwa/nuxt"],

  /*
   * The progressive web app, which is how most people here will use this.
   *
   * A phone with the application on its home screen behaves like an
   * application: it opens without browser furniture, survives being closed,
   * and can be told something happened. Nothing here changes what the software
   * does — it changes whether somebody can keep it.
   */
  pwa: {
    registerType: "prompt",

    manifest: {
      name: "TDC",
      short_name: "TDC",
      description:
        "Chat, project tracking, and a directory connecting developers " +
        "with the people who need them.",
      // Standalone, so an installed application has no address bar competing
      // with its own header for the top of a small screen.
      display: "standalone",
      orientation: "portrait",
      start_url: "/",
      scope: "/",
      /*
       * The dark ground, because the palette defaults to dark and the splash
       * a phone draws from these values appears before any stylesheet does.
       * A light splash followed by a dark application is a flash by another
       * route.
       */
      background_color: "#111827",
      theme_color: "#111827",
      icons: [
        { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
        { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
        {
          src: "/icon-512-maskable.png",
          sizes: "512x512",
          type: "image/png",
          purpose: "maskable",
        },
      ],
    },

    workbox: {
      // The built assets, which carry content hashes and so can be cached
      // until they are replaced.
      globPatterns: ["**/*.{js,css,html,svg,png,ico,woff2}"],

      /*
       * What must never be answered from the cached shell.
       *
       * A single-page application tells the service worker to answer any
       * navigation with its shell, which is what makes a deep link work
       * offline. Left unqualified, that swallows the API and the live
       * connection too: a request for a conversation would be answered with
       * the application's own HTML, and a client would parse a page as though
       * it were messages.
       *
       * The live connection is the worse half. A WebSocket upgrade answered
       * with a cached document does not fail in a way anything reports — it
       * simply never connects, and the application looks merely quiet.
       */
      navigateFallback: "/",
      navigateFallbackDenylist: [
        /*
         * Everything the server answers, which includes the live connection:
         * it is served at /api/live rather than on a path of its own, so one
         * rule covers both. Guarding /ws and /socket as well would be guarding
         * paths this server does not have — a rule that protects nothing is a
         * rule somebody later trusts.
         */
        /^\/api\//,
        /*
         * Anything with an extension, which a browser fetches rather than
         * navigates to. A download answered with the shell is a corrupt file
         * rather than an error, and a corrupt file is harder to diagnose.
         */
        /\.[a-z0-9]+$/i,
      ],

      // A shell replaced by a newer one leaves its old cache behind otherwise,
      // and a phone accumulates every version it has ever seen.
      cleanupOutdatedCaches: true,
      /*
       * Four megabytes, which is more than any built asset here and less than
       * a phone will resent. Without a limit a single large chunk silently
       * fails to precache, and the application is offline-capable except for
       * the one file it needs most.
       */
      maximumFileSizeToCacheInBytes: 4 * 1024 * 1024,
    },

    client: {
      // The prompt is ours to show, so an update never interrupts somebody
      // mid-sentence. See 8.8.
      installPrompt: true,
    },

    devOptions: {
      // Off in development: a service worker caching a development build is a
      // stale application that survives a restart, which costs more time than
      // it ever saves.
      enabled: false,
      type: "module",
    },
  },

  css: ["~/assets/css/main.css"],

  components: [
    {
      path: "~/components",
      // The vendored shadcn directories each carry an index.ts barrel, which
      // the scanner would otherwise register as a second component under the
      // same name as the .vue file beside it. Components are imported from
      // those barrels explicitly, so only .vue files need auto-registering.
      extensions: [".vue"],
      pathPrefix: true,
    },
  ],

  runtimeConfig: {
    public: {
      // Empty means "same origin", which is how the production build is
      // served: the server serves the built application, so the API is on the
      // same host and the session cookie is first-party.
      //
      // In development the app and the API are separate processes, so this
      // points at the API. It must name the SAME HOST the app is served from
      // — localhost:3000 and 127.0.0.1:8080 are different hosts to a browser,
      // which would treat the session cookie as third-party and, under
      // SameSite=Lax, decline to send it back.
      apiBase: process.env.NUXT_PUBLIC_API_BASE ?? "http://localhost:8080",
    },
  },

  alias: {
    "@tdc/types": fileURLToPath(new URL("../../packages/types/src", import.meta.url)),
  },

  vite: {
    plugins: [tailwindcss()],
    server: {
      // Wails expects a predictable port; sharing the convention here keeps
      // the two apps consistent.
      strictPort: true,
    },
  },

  typescript: {
    typeCheck: false,
    strict: true,
  },
});
