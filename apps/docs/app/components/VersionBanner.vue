<script setup lang="ts">
const { current, stable, switchTo } = useDocsVersions();

const notice = computed(() => {
  if (!stable.value || current.value.version === stable.value.version) return null;
  return current.value.version === "beta"
    ? "You're reading the beta docs: what the next release is expected to do."
    : "You're reading the development docs: they describe work that may not be released yet.";
});
</script>

<template>
  <div
    v-if="notice && stable"
    class="bg-primary/10 text-sm border-b border-default px-4 py-2 text-center text-pretty"
  >
    {{ notice }}
    <a
      :href="stable.url"
      class="font-medium text-highlighted underline underline-offset-2"
      @click.prevent="switchTo(stable)"
      >Read the stable docs</a
    >
  </div>
</template>
