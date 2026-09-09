<script setup lang="ts">
/*
 * The desktop client.
 *
 * Deliberately thin: every screen comes from the shared layer, and this file
 * exists to mount them. Anything that appears here rather than there is
 * something the web client would not have, which is a decision worth making
 * on purpose rather than by drift.
 */
useHead({
  titleTemplate: (page?: string) => (page ? `${page} — TDC` : "TDC"),
});
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
