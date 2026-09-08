/**
 * The interface both clients share, as a Nuxt layer.
 *
 * A layer rather than a component library: Nuxt merges an extending
 * application's own directories over these, so a client can replace one
 * component or composable without forking the rest. The desktop client needs
 * exactly that — it shares the conversation and every screen around it, and
 * replaces only how bytes reach the server.
 *
 * Nothing here may reach for a transport. The one thing that differs between
 * the two clients is how a request is sent, and a composable that calls fetch
 * directly has made itself web-only without saying so.
 */
import { fileURLToPath } from "node:url";

/*
 * This layer's own directory.
 *
 * Relative paths in a layer's configuration are resolved against whichever
 * application is extending it, not against this file — so "./app/components"
 * silently pointed at the web application's directory and none of the layer's
 * components were registered at all.
 */
const here = fileURLToPath(new URL(".", import.meta.url));

export default defineNuxtConfig({
  modules: ["nuxt-i18n-micro", "@nuxt/icon"],

  /*
   * Icons, resolved offline and bundled into the client.
   *
   * The default fetches from Iconify's API at runtime, which this cannot rely
   * on: a self-hosted server on a network that does not reach the public
   * internet would show an interface with holes where its icons were. The
   * collection is a development dependency, so only the icons actually used
   * reach the built application.
   */
  icon: {
    // Never the network. An icon that is missing at build time should be a
    // build somebody fixes, not a gap somebody discovers.
    provider: "none",
    clientBundle: {
      /*
       * Scanning, which is what puts any icon in the bundle at all.
       *
       * Without this block nothing is bundled and every icon is fetched from
       * the network at runtime — measured, not assumed: removing it leaves the
       * built application with none of them.
       *
       * The layer's own path is named alongside the application's. It turns
       * out not to be strictly required — Nuxt resolves a layer's components
       * into the application's tree, so the scanner reaches them either way —
       * but the desktop client will extend this layer from a different
       * directory, and a scan that depends on that resolution continuing to
       * work is one nobody would think to check until the icons were gone.
       */
      scan: {
        globInclude: ["app/**/*.{vue,ts}", "../../packages/ui/app/**/*.{vue,ts}"],
        globExclude: ["node_modules", "dist", ".output", ".nuxt"],
      },
      /*
       * Bounded, because bundling icons trades requests for weight and an
       * unbounded trade is one nobody notices making. Over this, the build
       * says so rather than quietly shipping it.
       */
      sizeLimitKb: 128,
    },
  },

  /*
   * English and Swahili, which is what this community actually speaks.
   *
   * Configured in the layer rather than per client so both get the same
   * translations from the same files — a desktop client with its own copy
   * would drift, and the drift would show as one client saying something the
   * other does not.
   */
  i18n: {
    locales: [
      { code: "en", iso: "en-GB", dir: "ltr", displayName: "English" },
      { code: "sw", iso: "sw-TZ", dir: "ltr", displayName: "Kiswahili" },
    ],
    defaultLocale: "en",
    /*
     * English is what an untranslated key falls back to.
     *
     * Never a key: somebody meeting "rooms.empty.title" has met a bug wearing
     * the interface's clothes. English is not ideal for a Swahili speaker, and
     * it is a great deal better than punctuation.
     */
    fallbackLocale: "en",
    // Said out loud in development, so a missing translation is noticed by
    // whoever added the string rather than by whoever reads it.
    missingWarn: true,
    /*
     * No locale in the URL. The desktop client has no URLs to put one in, and
     * a shared layer whose routing depends on a browser is a layer with one
     * consumer. The choice lives in a cookie, which both clients have.
     */
    strategy: "no_prefix",
    translationDir: "locales",
  },

  /*
   * Named, so files in either client can reach the layer's own modules as
   * #layers/ui/... — the ~ alias resolves to whichever application is
   * extending, which is exactly wrong for a file that lives here.
   */
  $meta: { name: "ui" },

  components: [
    {
      path: `${here}app/components`,
      // The vendored shadcn directories carry index.ts barrels, which the
      // scanner would otherwise register as a second component under the same
      // name as the .vue file beside it.
      extensions: [".vue"],
      pathPrefix: true,
    },
  ],
});
