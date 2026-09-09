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

/*
 * The colours, resolved the same way the web client resolves them: this
 * person's choice over the operator's default over the palette's own. Both
 * clients call the one function, so a change to the order cannot apply to one
 * of them and not the other.
 */
useAppearanceSync();
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
