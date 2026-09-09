export default defineNuxtConfig({
  compatibilityDate: "2026-08-29",

  /*
   * The interface shared with the desktop client.
   *
   * Nuxt merges this application's own directories over the layer's, so
   * anything here wins where both define the same thing — which is how the
   * desktop client will replace the transport and keep everything else.
   */
  extends: ["../../packages/ui"],

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
      /*
       * The web manifest, linked here rather than through the module's own
       * component.
       *
       * That component renders with the application, and this application does
       * not render on the server — so the link would arrive after the browser
       * had already decided whether anything was installable. The manifest was
       * being written, served, and read by nothing: a phone simply never
       * offered to add it to a home screen, which looks like the feature not
       * existing rather than a missing line.
       */
      link: [{ rel: "manifest", href: "/manifest.webmanifest" }],
      meta: [
        {
          name: "description",
          content:
            "Chat, project tracking, and a directory connecting developers " +
            "with the people who need them.",
        },
      ],
    },
  },

  devtools: { enabled: true },

  modules: ["@vite-pwa/nuxt"],

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

    /*
     * The service worker is written out rather than generated.
     *
     * A generated worker cannot be given a push handler, and receiving a
     * notification while the application is closed is the whole reason a phone
     * installs this. Everything the generated one did is kept; what is added
     * is the push and notification handling in service-worker/sw.ts.
     */
    strategies: "injectManifest",
    // Relative to Nuxt's srcDir, which is app/ — not the project root.
    srcDir: "service-worker",
    filename: "sw.ts",

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

    /*
     * What is precached into the worker built from sw.ts.
     *
     * The routing rules that used to sit here live in that file now, because
     * the worker decides them itself — keeping them in both places would be
     * two places to change one rule, and the one that mattered would be the
     * one somebody missed.
     */
    injectManifest: {
      // The built assets, which carry content hashes and so can be cached
      // until they are replaced.
      globPatterns: ["**/*.{js,css,html,svg,png,ico,woff2}"],
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

      /*
       * How often a running application looks for a new version, in seconds.
       *
       * Without this the browser checks only on navigation, and a chat
       * application is precisely the kind that is left open for days — so a
       * tab could sit on a version from a week ago and never be told. Hourly
       * is frequent enough that nobody is far behind and rare enough to cost
       * nothing: the check is one request for the worker, skipped entirely
       * while offline or while an update is already installing.
       *
       * Finding a new version only raises the prompt. Applying it stays
       * somebody's own decision, which is what registerType "prompt" is for.
       */
      periodicSyncForUpdates: 60 * 60,
    },

    devOptions: {
      // Off in development: a service worker caching a development build is a
      // stale application that survives a restart, which costs more time than
      // it ever saves.
      enabled: false,
      type: "module",
    },
  },

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

  vite: {
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
