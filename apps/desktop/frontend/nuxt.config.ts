import { fileURLToPath } from "node:url";

const here = fileURLToPath(new URL(".", import.meta.url));

export default defineNuxtConfig({
  compatibilityDate: "2026-08-29",

  future: { compatibilityVersion: 5 },

  /*
   * The same interface the web client shows.
   *
   * Everything a person sees comes from the layer. What this application adds
   * is the one thing that genuinely differs — how a request reaches the server
   * — plus the capabilities a browser cannot offer: a tray, a keychain, native
   * notifications.
   */
  extends: ["../../../packages/ui"],

  // There is no server here. Wails serves the built files from inside the
  // binary, and everything is decided in the webview.
  ssr: false,

  nitro: {
    output: {
      /*
       * Where Wails looks.
       *
       * `//go:embed all:frontend/dist` in main.go embeds this directory into
       * the binary at compile time. Nuxt writes to .output/public by default,
       * which the embed would not find — and the failure is a build that
       * succeeds and an application that opens onto nothing.
       */
      publicDir: "dist",
    },
  },

  devServer: {
    /*
     * The port `wails3 dev` expects, from WAILS_VITE_PORT.
     *
     * Fixed rather than chosen at random, because the Go side has to know
     * where to point the webview before the frontend has started.
     */
    port: Number(process.env.WAILS_VITE_PORT ?? 9245),
  },

  /*
   * The generated bindings, under a name worth typing.
   *
   * `wails3 generate bindings` writes TypeScript mirroring the Go services
   * into frontend/bindings, nested by import path — so reaching the API
   * service from a composable otherwise means seven levels of `../`, which is
   * both unreadable and quietly wrong the moment a file moves.
   *
   * Absolute, for the reason the shared layer's paths are: a relative path in
   * a Nuxt configuration is resolved against whichever application is
   * extending, and this one is extending rather than extended.
   */
  alias: {
    "#bindings": `${here}bindings/github.com/AnoRebel/tdc/apps/desktop/internal`,
  },

  vite: {
    server: {
      // A port that is taken should be an error, not a silent move to another
      // one the Go side is not watching.
      strictPort: true,
    },
  },
});
