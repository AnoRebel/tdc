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
 * The colours, resolved from what the operator chose and what this person did.
 *
 * In the shared layer rather than here, so the desktop client resolves them
 * the same way. It used to be a watcher in this file that applied the
 * operator's accent and cleared the custom property when the operator had set
 * none — which wiped a person's own accent every time server information
 * reloaded.
 */
useAppearanceSync();
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>

  <!--
    A new version, offered rather than applied. Web-only: it updates the
    service worker, which a desktop webview does not have — the desktop client
    offers its own update through the menu, from the updater service. Rendered
    here rather than in the shared layout so that the layout carries nothing a
    desktop webview cannot run.
  -->
  <UpdatePrompt />
</template>
