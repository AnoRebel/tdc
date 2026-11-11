<script setup lang="ts">
import "@videojs/html/video/player";
import "@videojs/html/video/neutral-skin";
import "#layers/ui/app/lib/player-sw";
import { Maximize2 } from "lucide-vue-next";

const { $ts, $getLocale } = useI18n();

/**
 * A video, played with Video.js's controls rather than the browser's.
 *
 * Used in two places: inline where it sits in the conversation, and filling
 * the attachment viewer. Inline rather than only in a lightbox: someone
 * watching a screen recording of a bug usually wants to keep reading what was
 * said about it. The enlarge control is there for when the detail matters more
 * than the context.
 *
 * Video.js keeps fullscreen, picture-in-picture, captions and playback speed,
 * and gives them the same shape and colour in every browser and in the
 * desktop's webview, which the native bar did not.
 */

const props = defineProps<{
  src: string;
  /** Used for the accessible name and the poster's aspect ratio. */
  title: string;
  width?: number;
  height?: number;
  /** Offers a control to open the file in the larger viewer. */
  expandable?: boolean;
  autoplay?: boolean;
  /** Fill the space it is given, as in the viewer, rather than sit inline. */
  fill?: boolean;
}>();

const emit = defineEmits<{ expand: [] }>();

const media = useTemplateRef<HTMLVideoElement>("media");

/** The viewer plays its own copy; two soundtracks at once is nobody's intent. */
function expand() {
  media.value?.pause();
  emit("expand");
}

/**
 * The space the video will occupy, so the conversation does not jump when it
 * loads. Falls back to widescreen, which is what most recordings are.
 */
const shape = computed(() =>
  props.fill
    ? { width: "100%", height: "100%" }
    : { aspectRatio: props.width && props.height ? `${props.width} / ${props.height}` : "16 / 9" },
);
</script>

<template>
  <div class="group/video relative" :class="props.fill ? 'h-full w-full' : 'w-md max-w-full'">
    <media-i18n :lang="$getLocale()">
      <video-player>
        <video-neutral-skin class="tdc-video" :style="shape">
          <!-- crossorigin is required, not cosmetic: a media element loading
               from another origin sends no cookies unless told to, and the
               session is a cookie. Without it, video silently never loads in
               development. -->
          <video
            ref="media"
            :src="props.src"
            :aria-label="props.title"
            crossorigin="use-credentials"
            preload="metadata"
            playsinline
            :autoplay="props.autoplay"
          />
        </video-neutral-skin>
      </video-player>
    </media-i18n>

    <button
      v-if="props.expandable"
      type="button"
      class="absolute top-2 right-2 z-10 grid size-7 place-items-center rounded-md bg-black/50 text-white opacity-0 transition-opacity group-hover/video:opacity-100 focus-visible:opacity-100"
      :aria-label="$ts('attachment.openInViewer')"
      :title="$ts('attachment.openInViewer')"
      @click="expand"
    >
      <Maximize2 class="size-3.5" />
    </button>
  </div>
</template>

<style>
.tdc-video {
  display: block;
  --media-accent-color: var(--primary);
  --media-accent-text-color: var(--primary-foreground);
  --media-font-family: inherit;
  --media-border-radius: 0.75rem;
  /* Contain, not the skin's cover: a portrait recording in the widescreen
     fallback shape would otherwise lose its top and bottom. */
  --media-object-fit: contain;
  --media-object-position: center;
  background: black;
  border-radius: 0.75rem;
}
</style>
