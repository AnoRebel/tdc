<script setup lang="ts">
import { onKeyStroke } from "@vueuse/core";
import {
  FlipHorizontal2,
  LocateFixed,
  RotateCcw,
  RotateCw,
  ZoomIn,
  ZoomOut,
} from "lucide-vue-next";
import { isApiError } from "@tdc/types";

import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Framing a picture before it becomes somebody's face.
 *
 * Drag to move it, the slider or the wheel to zoom, buttons to turn, flip and
 * centre it; arrow keys and + and − do the same from the keyboard. What is
 * inside the frame is what is kept: drawn again at 512 pixels square, so what
 * is sent is small whatever was chosen, and carries none of what the original
 * file did besides its pixels — a phone photograph's location among them.
 * The server checks and writes it again regardless.
 */

const props = defineProps<{ file: File }>();
const emit = defineEmits<{ done: [url: string]; cancel: [] }>();

const { $t, $ts } = useI18n();
const api = useApi();
const { user } = useAuth();
const avatars = useAvatars();

/** The frame on screen, in CSS pixels, and the picture kept, in pixels. */
const VIEW = 280;
const OUTPUT = 512;

const canvas = useTemplateRef<HTMLCanvasElement>("canvas");
const image = shallowRef<HTMLImageElement | null>(null);
const unreadable = ref(false);

const zoom = ref(1);
const offset = reactive({ x: 0, y: 0 });
const quarterTurns = ref(0);
const flipped = ref(false);

const saving = ref(false);
const refusal = ref("");

let objectUrl = "";
onMounted(async () => {
  objectUrl = URL.createObjectURL(props.file);
  const loaded = new Image();
  loaded.src = objectUrl;
  try {
    await loaded.decode();
    image.value = loaded;
  } catch {
    unreadable.value = true;
  }
});
onUnmounted(() => URL.revokeObjectURL(objectUrl));

/** The picture's size once turned: a quarter turn swaps its sides. */
function turnedSize(picture: HTMLImageElement) {
  const sideways = quarterTurns.value % 2 === 1;
  return sideways
    ? { width: picture.naturalHeight, height: picture.naturalWidth }
    : { width: picture.naturalWidth, height: picture.naturalHeight };
}

/** The scale at which the picture just covers a frame of this size, zoomed. */
function scaleFor(picture: HTMLImageElement, frame: number) {
  const { width, height } = turnedSize(picture);
  return Math.max(frame / width, frame / height) * zoom.value;
}

/**
 * Keeps the frame covered: the picture may move only as far as it overhangs,
 * so there is never an empty edge in what is kept.
 */
function clamp() {
  const picture = image.value;
  if (!picture) return;
  const { width, height } = turnedSize(picture);
  const scale = scaleFor(picture, VIEW);
  const spareX = Math.max(0, (width * scale - VIEW) / 2);
  const spareY = Math.max(0, (height * scale - VIEW) / 2);
  offset.x = Math.min(spareX, Math.max(-spareX, offset.x));
  offset.y = Math.min(spareY, Math.max(-spareY, offset.y));
}

/** Draws the picture into a square of the given size, as the frame shows it. */
function paint(context: CanvasRenderingContext2D, size: number) {
  const picture = image.value;
  if (!picture) return;
  const ratio = size / VIEW;
  const scale = scaleFor(picture, size);
  context.save();
  // A ground for a picture with transparent parts: it is kept as a photograph.
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, size, size);
  context.translate(size / 2 + offset.x * ratio, size / 2 + offset.y * ratio);
  context.rotate((quarterTurns.value * Math.PI) / 2);
  context.scale(flipped.value ? -scale : scale, scale);
  context.drawImage(picture, -picture.naturalWidth / 2, -picture.naturalHeight / 2);
  context.restore();
}

watchEffect(() => {
  const element = canvas.value;
  if (!element || !image.value) return;
  // Read so the effect follows them.
  void [zoom.value, offset.x, offset.y, quarterTurns.value, flipped.value];
  const density = window.devicePixelRatio || 1;
  element.width = VIEW * density;
  element.height = VIEW * density;
  const context = element.getContext("2d");
  if (!context) return;
  context.scale(density, density);
  paint(context, VIEW);
});

watch([zoom, quarterTurns], clamp);

function setZoom(value: number) {
  zoom.value = Math.min(4, Math.max(1, Math.round(value * 100) / 100));
}

/* Dragging, with the pointer captured so it keeps moving outside the frame. */
let dragFrom: { x: number; y: number; at: { x: number; y: number } } | null = null;
function startDrag(event: PointerEvent) {
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  dragFrom = { x: event.clientX, y: event.clientY, at: { ...offset } };
}
function drag(event: PointerEvent) {
  if (!dragFrom) return;
  offset.x = dragFrom.at.x + event.clientX - dragFrom.x;
  offset.y = dragFrom.at.y + event.clientY - dragFrom.y;
  clamp();
}
function endDrag() {
  dragFrom = null;
}

function wheel(event: WheelEvent) {
  setZoom(zoom.value - event.deltaY / 400);
}

/** The keyboard does what the pointer does. */
function keys(event: KeyboardEvent) {
  const step = event.shiftKey ? 32 : 8;
  const moves: Record<string, () => void> = {
    ArrowLeft: () => (offset.x -= step),
    ArrowRight: () => (offset.x += step),
    ArrowUp: () => (offset.y -= step),
    ArrowDown: () => (offset.y += step),
    "+": () => setZoom(zoom.value + 0.1),
    "=": () => setZoom(zoom.value + 0.1),
    "-": () => setZoom(zoom.value - 0.1),
  };
  const move = moves[event.key];
  if (!move) return;
  event.preventDefault();
  move();
  clamp();
}

function turn(by: 1 | -1) {
  quarterTurns.value = (quarterTurns.value + by + 4) % 4;
}

/*
 * Mirrored left to right as it is on screen. The picture is drawn turned and
 * then mirrored on its own axis, so once it has been turned that axis is not
 * the screen's; reversing the turn as well gives the mirror image of exactly
 * what is shown.
 */
function flip() {
  flipped.value = !flipped.value;
  quarterTurns.value = (4 - quarterTurns.value) % 4;
}

function centre() {
  offset.x = 0;
  offset.y = 0;
}

function reset() {
  zoom.value = 1;
  quarterTurns.value = 0;
  flipped.value = false;
  centre();
}

/** The framed picture, drawn again at its kept size, as base64. */
async function framed(): Promise<string> {
  const output = document.createElement("canvas");
  output.width = OUTPUT;
  output.height = OUTPUT;
  const context = output.getContext("2d");
  if (!context) throw new Error("no canvas");
  paint(context, OUTPUT);
  const blob = await new Promise<Blob | null>((resolve) =>
    output.toBlob(resolve, "image/jpeg", 0.9),
  );
  if (!blob) throw new Error("no picture");
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let binary = "";
  for (let at = 0; at < bytes.length; at += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  }
  return btoa(binary);
}

async function save() {
  saving.value = true;
  refusal.value = "";
  try {
    const { url } = await api.put<{ url: string }>("/api/me/avatar", { image: await framed() });
    if (user.value) avatars.setOwn(user.value.address, url);
    emit("done", url);
  } catch (cause) {
    refusal.value = isApiError(cause) ? cause.message : $ts("avatar.couldNotSave");
  } finally {
    saving.value = false;
  }
}

/* Focus into the dialog, and Escape out of it, as the other dialogs do. */
const panel = useTemplateRef<HTMLElement>("panel");
let returnTo: HTMLElement | null = null;
onMounted(() => {
  returnTo = document.activeElement as HTMLElement | null;
  void nextTick(() => panel.value?.focus());
});
onUnmounted(() => returnTo?.focus?.());
onKeyStroke("Escape", (event) => {
  event.preventDefault();
  emit("cancel");
});

const titleId = useId();
const zoomId = useId();
</script>

<template>
  <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    @click.self="emit('cancel')"
  >
    <div
      ref="panel"
      class="motion-pop-in w-[min(24rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl outline-none"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
    >
      <h2 :id="titleId" class="text-sm font-semibold">{{ $t("avatar.frameTitle") }}</h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("avatar.frameHint") }}</p>

      <p v-if="unreadable" role="alert" class="mt-4 text-xs text-destructive">
        {{ $t("avatar.unreadable") }}
      </p>

      <template v-else>
        <div class="relative mx-auto mt-4 size-[280px] overflow-hidden rounded-lg bg-muted">
          <canvas
            ref="canvas"
            class="size-full cursor-grab touch-none active:cursor-grabbing focus-visible:outline-none"
            tabindex="0"
            role="img"
            :aria-label="$ts('avatar.frameLabel')"
            @pointerdown="startDrag"
            @pointermove="drag"
            @pointerup="endDrag"
            @pointercancel="endDrag"
            @wheel.prevent="wheel"
            @keydown="keys"
          />
          <!-- The shape a picture is drawn in, with what falls outside it dimmed. -->
          <span
            class="pointer-events-none absolute inset-0 rounded-[22%] shadow-[0_0_0_9999px_oklch(0_0_0/0.45)] ring-2 ring-white/80"
            aria-hidden="true"
          />
        </div>

        <div class="mt-3 flex items-center gap-2">
          <ZoomOut class="size-3.5 text-muted-foreground" aria-hidden="true" />
          <label :for="zoomId" class="sr-only">{{ $t("avatar.zoom") }}</label>
          <input
            :id="zoomId"
            type="range"
            min="1"
            max="4"
            step="0.01"
            class="flex-1 accent-primary"
            :value="zoom"
            @input="setZoom(Number(($event.target as HTMLInputElement).value))"
          />
          <ZoomIn class="size-3.5 text-muted-foreground" aria-hidden="true" />
        </div>

        <div class="mt-3 flex flex-wrap justify-center gap-1">
          <Button variant="ghost" size="icon" :label="$ts('avatar.turnLeft')" @click="turn(-1)">
            <RotateCcw class="size-4" />
          </Button>
          <Button variant="ghost" size="icon" :label="$ts('avatar.turnRight')" @click="turn(1)">
            <RotateCw class="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            :label="$ts('avatar.flip')"
            :aria-pressed="flipped"
            @click="flip"
          >
            <FlipHorizontal2 class="size-4" />
          </Button>
          <Button variant="ghost" size="icon" :label="$ts('avatar.centre')" @click="centre">
            <LocateFixed class="size-4" />
          </Button>
          <Button variant="ghost" @click="reset">{{ $t("avatar.reset") }}</Button>
        </div>
      </template>

      <p v-if="refusal" role="alert" class="mt-3 text-xs text-destructive">{{ refusal }}</p>

      <div class="mt-4 flex justify-end gap-2">
        <Button variant="outline" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
        <Button :disabled="saving || !image" @click="save">
          {{ saving ? $t("avatar.saving") : $t("avatar.use") }}
        </Button>
      </div>
    </div>
  </div>
</template>
