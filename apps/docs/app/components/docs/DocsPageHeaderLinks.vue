<script setup lang="ts">
/*
 * Replaces the layer's page actions. Its menu button had no name, and most of
 * its entries were for AI tools and an MCP server this site does not offer.
 * What stays: copy the guide as markdown, or open it as markdown.
 */
const route = useRoute();
const toast = useToast();

const raw = computed(() => `/raw${route.path}.md`);
const copied = ref(false);

async function copyPage() {
  await navigator.clipboard.writeText(await $fetch<string>(raw.value));
  copied.value = true;
  toast.add({ title: "Copied this page as markdown", icon: "i-lucide-check" });
  setTimeout(() => (copied.value = false), 2000);
}

const items = computed(() => [
  { label: "View as markdown", icon: "i-simple-icons-markdown", to: raw.value, target: "_blank" },
]);
</script>

<template>
  <UFieldGroup size="sm">
    <UButton
      label="Copy page"
      :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
      color="neutral"
      variant="soft"
      @click="copyPage"
    />
    <UDropdownMenu :items="items" :content="{ align: 'end' }">
      <UButton
        icon="i-lucide-chevron-down"
        color="neutral"
        variant="soft"
        class="border-l border-muted"
        aria-label="More ways to read this page"
      />
    </UDropdownMenu>
  </UFieldGroup>
</template>
