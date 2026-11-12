export default defineAppConfig({
  /*
   * Off, deliberately. The layer builds its "Edit this page" link as
   * <repo>/edit/<branch>/content/<file>, and the guides are not in content/.
   * The site's own links (DocsAsideRightBottom, AppHeaderCTA) know where they are.
   */
  github: false,

  header: {
    title: "TDC",
  },

  seo: {
    title: "TDC documentation",
    description:
      "Using, hosting and building on TDC, the self-hostable platform of the Tanzania Developers Community.",
  },

  toc: {
    title: "On this page",
  },

  ui: {
    colors: {
      // The application's own accent: oklch(0.53 0.16 269) is Tailwind's indigo.
      primary: "indigo",
      neutral: "zinc",
    },
  },
});
