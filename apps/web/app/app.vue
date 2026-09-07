<script setup lang="ts">
// The session is resolved by the auth middleware on protected routes, so that
// it happens before a page decides whether to render or redirect. Public pages
// do not need it resolved first.
//
// Styling lives entirely in assets/css/main.css. Nothing global belongs here:
// a second palette defined in this file would silently override the design
// system's tokens, and a bare `button` rule would fight every `.btn` class.

/*
 * The title template, here rather than in nuxt.config.
 *
 * A function in the config does not survive serialisation into the built
 * config — it is silently dropped, and every page ends up showing only its own
 * title with no product name. Here it is ordinary code that runs.
 *
 * A function rather than a "%s" string because the string form has no null
 * branch: a page setting no title would render " — TDC" with a dangling
 * separator, and setting a default title instead produces "TDC — TDC".
 */
useHead({
  titleTemplate: (page?: string) => (page ? `${page} — TDC` : "TDC"),
});

/*
 * The operator's accent colour, applied as a custom property.
 *
 * Set on the root element rather than substituted into a stylesheet, so
 * nothing is rebuilt and a value the server would not accept simply never
 * arrives. The server validates it to the shapes a colour actually takes,
 * which is what stops a setting becoming a declaration of its own.
 */
const { branding } = useServerInfo();

watchEffect(() => {
  if (import.meta.server) return;

  const accent = branding.value.accent;
  if (accent) {
    document.documentElement.style.setProperty("--primary", accent);
  } else {
    document.documentElement.style.removeProperty("--primary");
  }
});
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
