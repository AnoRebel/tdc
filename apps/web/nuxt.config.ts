import { fileURLToPath } from "node:url";

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

  css: [],

  runtimeConfig: {
    public: {
      // Empty means "same origin", which is how the production build is
      // served. Development points at the local server.
      apiBase: "",
    },
  },

  alias: {
    "@tdc/types": fileURLToPath(new URL("../../packages/types/src", import.meta.url)),
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
