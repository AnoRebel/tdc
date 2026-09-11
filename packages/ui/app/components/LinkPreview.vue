<script setup lang="ts">
import type { LinkPreview } from "@tdc/types";
import { ExternalLink } from "lucide-vue-next";

/**
 * A card summarising a link someone posted.
 *
 * Nothing is shown until the server answers, and nothing is shown if it has
 * nothing to say — most links are to pages with no metadata, and a message is
 * perfectly good without a card under it. An empty or broken card would be
 * worse than none.
 */

const props = defineProps<{ url: string }>();

const preview = ref<LinkPreview | null>(null);
/**
 * The image is a URL on whatever site was linked, never something this server
 * stored, so it may simply fail. When it does the card keeps its text rather
 * than showing a broken frame.
 */
const imageFailed = ref(false);

const { preview: fetchPreview } = useLinkPreview();

onMounted(async () => {
  preview.value = await fetchPreview(props.url);
});

/** The site's own name where it gives one, otherwise its host. */
const source = computed(() => {
  if (preview.value?.siteName) return preview.value.siteName;

  try {
    return new URL(preview.value?.url ?? props.url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
});
</script>

<template>
  <a
    v-if="preview"
    :href="preview.url"
    target="_blank"
    rel="noopener noreferrer"
    class="mt-1.5 flex max-w-md gap-3 overflow-hidden rounded-lg border bg-surface p-3 transition hover:border-border-strong hover:bg-accent"
  >
    <!-- Lazy, and never blocking the text: a card whose picture is slow should
         still be readable, and on metered data the words are what matter. -->
    <img
      v-if="preview.imageUrl && !imageFailed"
      :src="preview.imageUrl"
      alt=""
      loading="lazy"
      decoding="async"
      referrerpolicy="no-referrer"
      class="size-16 shrink-0 rounded-md object-cover"
      @error="imageFailed = true"
    />

    <span class="min-w-0 flex-1">
      <span class="flex items-center gap-1.5 text-[0.6875rem] text-muted-foreground">
        <ExternalLink class="size-3 shrink-0" />
        <span class="truncate">{{ source }}</span>
      </span>

      <span v-if="preview.title" class="mt-0.5 block truncate text-sm font-medium">
        {{ preview.title }}
      </span>

      <span
        v-if="preview.description"
        class="mt-0.5 line-clamp-2 block text-xs text-muted-foreground"
      >
        {{ preview.description }}
      </span>
    </span>
  </a>
</template>
