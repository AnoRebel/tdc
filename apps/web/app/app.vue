<script setup lang="ts">
// The session is resolved by the auth middleware on protected routes, so that
// it happens before a page decides whether to render or redirect. Public pages
// do not need it resolved first.
//
// Styling lives entirely in assets/css/main.css. Nothing global belongs here:
// a second palette defined in this file would silently override the design
// system's tokens, and a bare `button` rule would fight every `.btn` class.
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
