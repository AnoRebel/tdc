<script setup lang="ts">
/*
 * Replaces the layer's page actions: copy the guide as markdown, open it as
 * markdown, or hand it to an AI assistant to ask questions about. The
 * layer's MCP entries are left out, since this site runs no MCP server.
 */
const route = useRoute();
const toast = useToast();

const raw = computed(() => `/raw${route.path}.md`);
const link = computed(() =>
  import.meta.client ? `${window.location.origin}${raw.value}` : raw.value,
);
const prompt = computed(() =>
  encodeURIComponent(`Read ${link.value} so I can ask questions about it.`),
);
const copied = ref(false);

async function copyPage() {
  await navigator.clipboard.writeText(await $fetch<string>(raw.value));
  copied.value = true;
  toast.add({ title: "Copied this page as markdown", icon: "i-lucide-check" });
  setTimeout(() => (copied.value = false), 2000);
}

async function copyLink() {
  await navigator.clipboard.writeText(link.value);
  toast.add({ title: "Copied the markdown link", icon: "i-lucide-check" });
}

/** Assistants that take a question in their address, so the page arrives asked about. */
const assistants = [
  {
    label: "Open in ChatGPT",
    icon: "i-simple-icons-openai",
    url: "https://chatgpt.com/?hints=search&q=",
  },
  { label: "Open in Claude", icon: "i-simple-icons-anthropic", url: "https://claude.ai/new?q=" },
  {
    label: "Open in Perplexity",
    icon: "i-simple-icons-perplexity",
    url: "https://www.perplexity.ai/search?q=",
  },
  { label: "Open in Grok", icon: "i-simple-icons-x", url: "https://grok.com/?q=" },
  {
    label: "Open in Le Chat",
    icon: "i-simple-icons-mistralai",
    url: "https://chat.mistral.ai/chat?q=",
  },
];

const items = computed(() => [
  [
    { label: "Copy markdown link", icon: "i-lucide-link", onSelect: copyLink },
    { label: "View as markdown", icon: "i-simple-icons-markdown", to: raw.value, target: "_blank" },
  ],
  assistants.map((assistant) => ({
    label: assistant.label,
    icon: assistant.icon,
    to: assistant.url + prompt.value,
    target: "_blank",
  })),
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
