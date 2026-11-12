<script setup lang="ts">
/*
 * Replaces the layer's (which is empty here: no AI assistant, no extra links).
 * Edits go to the branch of the version being read — Studio is configured for
 * it, and the GitHub editor link names it.
 */
const route = useRoute();
const { docs, current, studio } = useDocsVersions();

const file = computed(() => `docs${route.path}.md`);

const links = computed(() => [
  ...(studio.value
    ? [
        {
          label: "Edit in Studio",
          icon: "i-lucide-pencil-line",
          to: `/_studio?redirect=${encodeURIComponent(route.path)}`,
          external: true,
        },
      ]
    : []),
  {
    label: `Edit on GitHub (${current.value.version})`,
    icon: "i-simple-icons-github",
    to: `https://github.com/${docs.repository}/edit/${current.value.version}/${file.value}`,
    target: "_blank",
  },
  {
    label: "Report a problem",
    icon: "i-lucide-circle-alert",
    to: `https://github.com/${docs.repository}/issues/new`,
    target: "_blank",
  },
]);
</script>

<template>
  <div class="space-y-6">
    <USeparator type="dashed" />
    <UPageLinks title="This page" :links="links" />
  </div>
</template>
