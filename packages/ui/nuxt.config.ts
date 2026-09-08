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
export default defineNuxtConfig({
  modules: ["nuxt-i18n-micro"],

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
      path: "./app/components",
      // The vendored shadcn directories carry index.ts barrels, which the
      // scanner would otherwise register as a second component under the same
      // name as the .vue file beside it.
      extensions: [".vue"],
      pathPrefix: true,
    },
  ],
});
