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
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

// A relative path, not an alias: this runs while the configuration is being
// evaluated, before Nuxt has resolved any of its own aliases.
import { PREPAINT_THEME_SCRIPT } from "./app/lib/theme";
import { PREPAINT_INTERFACE_SCRIPT } from "./app/lib/interface";

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
  /*
   * VueUse belongs here rather than in an application: the components in this
   * layer use its composables — onClickOutside for a popover that can be
   * dismissed — and a module registered by only one client is a layer that
   * builds for one client.
   */
  modules: ["nuxt-i18n-micro", "@nuxt/icon", "@vueuse/nuxt"],

  /*
   * The theme, which lives here because the components here depend on it.
   *
   * It used to sit in the web application, and the layer's components carried
   * classes that only resolved because that one application happened to define
   * them. The desktop client extending this layer got the components and none
   * of the tokens: text rendered in the light theme's near-black against a
   * dark window, which is legible enough in a screenshot to look like a
   * styling opinion rather than a missing stylesheet.
   *
   * Absolute, for the same reason the component path is: a relative path here
   * resolves against whichever application is extending.
   */
  css: [`${here}app/assets/css/main.css`],

  app: {
    head: {
      meta: [
        /*
         * The browser is told which themes exist, so its own chrome — the
         * scrollbars, the form controls it draws itself — matches rather than
         * rendering a light scrollbar beside a dark page. The desktop client
         * has a webview drawing exactly the same controls.
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
           * that takes. In a desktop window that flash is worse — the window
           * background is painted by the operating system before the webview
           * draws anything at all.
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
        {
          /*
           * Motion and density, decided before the first paint for the same
           * reason: somebody who turned motion off should not watch the first
           * screen animate in before the application has loaded enough to
           * know.
           */
          key: "interface-prepaint",
          innerHTML: PREPAINT_INTERFACE_SCRIPT,
        },
      ],
    },
  },

  vite: {
    // Tailwind v4 is a Vite plugin rather than a PostCSS one, and it has to be
    // registered wherever the stylesheet above is compiled — which is now
    // every application that extends this layer.
    plugins: [tailwindcss()],
  },

  alias: {
    /*
     * The shared types, by package name.
     *
     * Source rather than a build artefact: there is no build step for this
     * package, and a stale dist would be worse than none.
     */
    "@tdc/types": fileURLToPath(new URL("../types/src", import.meta.url)),
  },

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
