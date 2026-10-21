<script setup lang="ts">
import { Maximize2 } from "lucide-vue-next";

const { $ts } = useI18n();

/**
 * A video, played where it sits in the conversation.
 *
 * Inline rather than only in a lightbox: someone watching a screen recording of
 * a bug usually wants to keep reading what was said about it, and a viewer that
 * covers the conversation makes that impossible. The enlarge control is there
 * for when the detail matters more than the context.
 *
 * The browser's own controls are kept here, unlike audio. A video control bar
 * carries fullscreen, picture-in-picture, captions and track selection, all of
 * which the platform implements better than a reimplementation would, and it
 * sits over the video rather than beside it — so it costs no layout and does
 * not clash with the surrounding design the way a bare audio bar does.
 */

const props = defineProps<{
  src: string;
  /** Used for the accessible name and the poster's aspect ratio. */
  title: string;
  width?: number;
  height?: number;
  /** Offers a control to open the file in the larger viewer. */
  expandable?: boolean;
}>();

const emit = defineEmits<{ expand: [] }>();

/**
 * The space the video will occupy, so the conversation does not jump when it
 * loads. Falls back to widescreen, which is what most recordings are.
 */
const shape = computed(() => ({
  aspectRatio: props.width && props.height ? `${props.width} / ${props.height}` : "16 / 9",
}));
</script>

<template>
  <div class="group/video relative w-full max-w-md overflow-hidden rounded-xl border bg-black">
    <!-- crossorigin is required, not cosmetic: a media element loading from
         another origin sends no cookies unless told to, and the session is a
         cookie. Without it, video silently never loads in development. -->
    <video
      :src="props.src"
      :aria-label="props.title"
      crossorigin="use-credentials"
      controls
      preload="metadata"
      playsinline
      class="w-full"
      :style="shape"
    />

    <button
      v-if="props.expandable"
      type="button"
      class="absolute top-2 right-2 grid size-7 place-items-center rounded-md bg-black/50 text-white opacity-0 transition-opacity group-hover/video:opacity-100 focus-visible:opacity-100"
      :aria-label="$ts('attachment.openInViewer')"
      :title="$ts('attachment.openInViewer')"
      @click="emit('expand')"
    >
      <Maximize2 class="size-3.5" />
    </button>
  </div>
</template>
