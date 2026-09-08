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
