<script setup lang="ts">
import { Maximize2, Pause, Play, Volume2, VolumeX } from "lucide-vue-next";

/**
 * An audio player, rather than the browser's default controls.
 *
 * The native control is a different shape and colour in every browser, ignores
 * the application's palette, and on some platforms is large enough to dominate
 * a message. Voice notes and recordings are common enough in a community like
 * this to be worth building properly.
 *
 * The audio element still does the work; only the controls are ours.
 */

const props = defineProps<{
  src: string;
  /** Shown above the controls, so a person knows what they are playing. */
  title?: string;
  /**
   * Offers a control to open the file in the larger viewer. Set where the
   * player sits in the conversation; omitted inside the viewer itself, where
   * there is nowhere further to go.
   */
  expandable?: boolean;
}>();

const emit = defineEmits<{ expand: [] }>();

const audio = ref<HTMLAudioElement | null>(null);

const playing = ref(false);
const muted = ref(false);
const currentTime = ref(0);
const duration = ref(0);
/** True while a stream is still being fetched, so the seek bar says so. */
const buffering = ref(false);

/**
 * Playback speed.
 *
 * A recorded meeting is often listened to at speed, and a voice note in a
 * language you are still learning at less than speed. The presets are the ones
 * every podcast player settled on, so they need no explanation.
 */
const speeds = [0.5, 1, 1.5, 2] as const;
const speed = ref<number>(1);

function cycleSpeed() {
  const next = speeds[(speeds.indexOf(speed.value as (typeof speeds)[number]) + 1) % speeds.length];
  speed.value = next ?? 1;
  if (audio.value) audio.value.playbackRate = speed.value;
}

const progress = computed(() =>
  duration.value > 0 ? (currentTime.value / duration.value) * 100 : 0,
);

function toggle() {
  const element = audio.value;
  if (!element) return;

  if (element.paused) {
    void element.play();
  } else {
    element.pause();
  }
}

function toggleMute() {
  const element = audio.value;
  if (!element) return;

  element.muted = !element.muted;
  muted.value = element.muted;
}

/** Seeks to wherever along the bar was clicked. */
function seek(event: MouseEvent) {
  const element = audio.value;
  if (!element || !duration.value) return;

  const bar = event.currentTarget as HTMLElement;
  const rectangle = bar.getBoundingClientRect();
  const proportion = (event.clientX - rectangle.left) / rectangle.width;

  element.currentTime = Math.max(0, Math.min(1, proportion)) * duration.value;
}

/** Keyboard seeking, so the bar is usable without a pointer. */
function nudge(seconds: number) {
  const element = audio.value;
  if (!element || !duration.value) return;

  element.currentTime = Math.max(0, Math.min(duration.value, element.currentTime + seconds));
}

/**
 * Minutes and seconds. Hours are included only when there are any, so a
 * two-minute voice note does not read as "00:02:14".
 */
function clock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "0:00";

  const whole = Math.floor(seconds);
  const hours = Math.floor(whole / 3600);
  const minutes = Math.floor((whole % 3600) / 60);
  const remainder = whole % 60;

  const padded = String(remainder).padStart(2, "0");
  if (hours > 0) return `${hours}:${String(minutes).padStart(2, "0")}:${padded}`;
  return `${minutes}:${padded}`;
}
</script>

<template>
  <div class="w-full max-w-md rounded-xl border bg-surface p-3 shadow-sm">
    <p v-if="props.title" class="mb-2 truncate text-sm font-medium">{{ props.title }}</p>

    <!--
      crossorigin="use-credentials" is required, not cosmetic. A media element
      loading from another origin sends no cookies unless told to, and the
      session is a cookie — so in development, where the app and the API are on
      different ports, audio would silently never load. In production they
      share an origin and this is harmless.
    -->
    <audio
      ref="audio"
      :src="props.src"
      crossorigin="use-credentials"
      preload="metadata"
      class="sr-only"
      @play="playing = true"
      @pause="playing = false"
      @ended="playing = false"
      @timeupdate="currentTime = audio?.currentTime ?? 0"
      @loadedmetadata="
        duration = audio?.duration ?? 0;
        if (audio) audio.playbackRate = speed;
      "
      @waiting="buffering = true"
      @playing="buffering = false"
      @canplay="buffering = false"
    />

    <div class="flex items-center gap-3">
      <button
        type="button"
        class="btn btn-primary size-9 shrink-0 px-0"
        :aria-label="playing ? 'Pause' : 'Play'"
        @click="toggle"
      >
        <Spinner v-if="buffering" size="sm" />
        <Pause v-else-if="playing" class="size-4" />
        <Play v-else class="size-4" />
      </button>

      <span class="tabular shrink-0 text-xs text-muted-foreground">
        {{ clock(currentTime) }}
      </span>

      <!-- A slider role rather than a bare div: someone using a keyboard or a
           screen reader needs to be able to seek too. -->
      <div
        class="group/bar relative h-6 flex-1 cursor-pointer"
        role="slider"
        tabindex="0"
        :aria-valuenow="Math.round(currentTime)"
        aria-valuemin="0"
        :aria-valuemax="Math.round(duration)"
        :aria-valuetext="`${clock(currentTime)} of ${clock(duration)}`"
        aria-label="Seek"
        @click="seek"
        @keydown.left.prevent="nudge(-5)"
        @keydown.right.prevent="nudge(5)"
        @keydown.space.prevent="toggle"
      >
        <div class="absolute inset-x-0 top-1/2 h-1 -translate-y-1/2 rounded-full bg-muted">
          <div
            class="h-full rounded-full bg-primary transition-[width] duration-100"
            :style="{ width: `${progress}%` }"
          />
        </div>
        <!-- The handle appears on hover and focus, so the bar stays slim while
             still being obviously draggable when reached for. -->
        <span
          class="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary opacity-0 shadow-sm transition-opacity group-hover/bar:opacity-100 group-focus/bar:opacity-100"
          :style="{ left: `${progress}%` }"
        />
      </div>

      <span class="tabular shrink-0 text-xs text-muted-foreground">
        {{ clock(duration) }}
      </span>

      <!-- Speed cycles rather than opening a menu: four values, one of which
           is almost always wanted, is faster to reach by tapping through than
           by opening something. -->
      <button
        type="button"
        class="btn btn-ghost tabular h-8 shrink-0 px-1.5 text-xs font-medium"
        :aria-label="`Playback speed, currently ${speed} times`"
        :title="`Speed: ${speed}×`"
        @click="cycleSpeed"
      >
        {{ speed }}×
      </button>

      <button
        type="button"
        class="btn btn-ghost size-8 shrink-0 px-0"
        :aria-label="muted ? 'Unmute' : 'Mute'"
        @click="toggleMute"
      >
        <VolumeX v-if="muted" class="size-4" />
        <Volume2 v-else class="size-4" />
      </button>

      <button
        v-if="props.expandable"
        type="button"
        class="btn btn-ghost size-8 shrink-0 px-0"
        aria-label="Open in the viewer"
        title="Open in the viewer"
        @click="emit('expand')"
      >
        <Maximize2 class="size-3.5" />
      </button>
    </div>
  </div>
</template>
