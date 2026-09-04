import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";

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

  devtools: { enabled: true },

  modules: ["@vueuse/nuxt"],

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
