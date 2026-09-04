<script setup lang="ts">
import type { Attachment } from "@tdc/types";
import { Download, FileText, Music, Play } from "lucide-vue-next";

/**
 * A message's attachments, as they appear in the conversation.
 *
 * Images show their preview rather than the original — a reduced copy of a few
 * kilobytes against several megabytes, which is the difference between a
 * readable conversation and an unusable one on metered mobile data. The full
 * file is fetched only when someone asks to see it.
 */

const props = defineProps<{ attachments: Attachment[] }>();

const mediaUrl = useMediaUrl();

const viewing = ref<number | null>(null);

/** Attachments the viewer can display, so arrows move between those only. */
const viewable = computed(() => props.attachments.filter((attachment) => attachment.renderable));

function open(attachment: Attachment) {
  const position = viewable.value.indexOf(attachment);
  viewing.value = position >= 0 ? position : null;
}

function readableSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** The icon for a file with no preview, so the type is legible at a glance. */
function iconFor(attachment: Attachment) {
  if (attachment.contentType.startsWith("video/")) return Play;
  if (attachment.contentType.startsWith("audio/")) return Music;
  return FileText;
}

/**
 * The space a preview will occupy, so the conversation does not jump as images
 * arrive. Falls back to a modest box when the server did not record dimensions.
 */
function previewStyle(attachment: Attachment) {
  if (!attachment.width || !attachment.height) return {};

  return {
    aspectRatio: `${attachment.width} / ${attachment.height}`,
    maxWidth: "min(20rem, 100%)",
  };
}
</script>

<template>
  <div v-if="attachments.length" class="mt-1.5 flex flex-wrap gap-2">
    <template v-for="attachment in attachments" :key="attachment.id">
      <!-- An image with a preview: shown, and opened at full size on demand. -->
      <button
        v-if="attachment.previewUrl"
        type="button"
        class="group/att relative overflow-hidden rounded-lg border bg-surface-sunken transition hover:border-border-strong"
        :style="previewStyle(attachment)"
        :title="attachment.filename"
        @click="open(attachment)"
      >
        <img
          :src="mediaUrl(attachment.previewUrl)"
          :alt="attachment.filename"
          loading="lazy"
          decoding="async"
          class="size-full object-cover"
        />
        <span
          class="absolute inset-x-0 bottom-0 truncate bg-gradient-to-t from-black/70 to-transparent px-2 pt-6 pb-1.5 text-left text-[0.6875rem] text-white opacity-0 transition-opacity group-hover/att:opacity-100"
        >
          {{ attachment.filename }}
        </span>
      </button>

      <!-- Video and audio: no preview image, but still openable in place. -->
      <button
        v-else-if="attachment.renderable"
        type="button"
        class="flex items-center gap-2.5 rounded-lg border bg-surface px-3 py-2 text-left transition hover:border-border-strong hover:bg-accent"
        @click="open(attachment)"
      >
        <span class="grid size-8 shrink-0 place-items-center rounded-md bg-primary/12">
          <component :is="iconFor(attachment)" class="size-4 text-primary" />
        </span>
        <span class="min-w-0">
          <span class="block max-w-52 truncate text-sm font-medium">
            {{ attachment.filename }}
          </span>
          <span class="block text-xs text-muted-foreground">
            {{ readableSize(attachment.size) }}
          </span>
        </span>
      </button>

      <!-- Everything else: name, size and type, with a download. Never an
           error and never an empty frame. -->
      <a
        v-else
        :href="mediaUrl(attachment.url)"
        :download="attachment.filename"
        class="flex items-center gap-2.5 rounded-lg border bg-surface px-3 py-2 transition hover:border-border-strong hover:bg-accent"
      >
        <span class="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
          <component :is="iconFor(attachment)" class="size-4 text-muted-foreground" />
        </span>
        <span class="min-w-0">
          <span class="block max-w-52 truncate text-sm font-medium">
            {{ attachment.filename }}
          </span>
          <span class="block text-xs text-muted-foreground">
            {{ readableSize(attachment.size) }}
          </span>
        </span>
        <Download class="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </template>

    <AttachmentViewer
      v-if="viewing !== null"
      :attachments="viewable"
      :start-at="viewing"
      @close="viewing = null"
    />
  </div>
</template>
