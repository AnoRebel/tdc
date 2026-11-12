<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { MousePointer2 } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import type { ControlInput } from "#layers/ui/app/composables/useRemoteControlHost";

/**
 * Remote control, from the controller's side, over the presenter's screen:
 * asking, what came of it, and — while control is given — the screen taking
 * the pointer and the keyboard, every move and key sent to the presenter's
 * computer as a fraction of their screen.
 *
 * The keyboard is taken only while the screen has focus, so the person can
 * always tab or click away from it; Escape is sent on like any other key.
 */

const props = defineProps<{ video: HTMLVideoElement | null; presenter: string }>();
const { $t, $ts } = useI18n();
const c = useCall();
const r = useRemoteControl();

const who = computed(() => {
  const person = c.call.value?.participants.find((each) => each.userId === props.presenter);
  return person ? nameOf(person) : "";
});
const state = computed(() =>
  r.mine.value?.sharer === props.presenter ? r.mine.value.state : null,
);
const controlling = computed(() => state.value === "controlling");

const refusals = computed(() => ({
  browser: $ts("calls.control.refused.browser", { name: who.value }),
  "not-screen": $ts("calls.control.refused.not-screen", { name: who.value }),
  wayland: $ts("calls.control.refused.wayland", { name: who.value }),
  "no-display": $ts("calls.control.refused.no-display", { name: who.value }),
  "no-xtest": $ts("calls.control.refused.no-xtest", { name: who.value }),
  accessibility: $ts("calls.control.refused.accessibility", { name: who.value }),
  platform: $ts("calls.control.refused.platform", { name: who.value }),
  forwarded: $ts("calls.control.refused.forwarded", { name: who.value }),
}));
const outcome = computed(() => {
  switch (state.value) {
    case "asking":
      return $ts("calls.control.asking", { name: who.value });
    case "declined":
      return $ts("calls.control.declined", { name: who.value });
    case "revoked":
      return $ts("calls.control.revoked", { name: who.value });
    case "refused":
      return refusals.value[r.mine.value?.reason ?? "platform"] ?? refusals.value.platform;
  }
  return "";
});

const blank: ControlInput = {
  kind: "move",
  x: 0,
  y: 0,
  button: 0,
  down: false,
  dx: 0,
  dy: 0,
  code: "",
};

/** Where on the presenter's screen a point on the video is, letterboxing taken off. */
function at(event: MouseEvent): { x: number; y: number } | null {
  const video = props.video;
  if (!video?.videoWidth) return null;
  const box = video.getBoundingClientRect();
  const scale = Math.min(box.width / video.videoWidth, box.height / video.videoHeight);
  const width = video.videoWidth * scale;
  const height = video.videoHeight * scale;
  const x = (event.clientX - box.left - (box.width - width) / 2) / width;
  const y = (event.clientY - box.top - (box.height - height) / 2) / height;
  if (x < 0 || x > 1 || y < 0 || y > 1) return null;
  return { x, y };
}

// Moves at most once a frame, so a fast mouse does not flood the channel.
let pending: { x: number; y: number } | null = null;
let frame = 0;
function onMove(event: PointerEvent) {
  if (!controlling.value) return;
  pending = at(event);
  if (frame) return;
  frame = requestAnimationFrame(() => {
    frame = 0;
    if (pending) r.send({ ...blank, kind: "move", ...pending });
  });
}
function onButton(event: PointerEvent, down: boolean) {
  if (!controlling.value) return;
  const point = at(event);
  if (!point) return;
  event.preventDefault();
  (event.currentTarget as HTMLElement).focus();
  r.send({ ...blank, kind: "button", ...point, button: event.button, down });
}
function onWheel(event: WheelEvent) {
  if (!controlling.value) return;
  event.preventDefault();
  r.send({ ...blank, kind: "wheel", dx: event.deltaX, dy: event.deltaY });
}
function onKey(event: KeyboardEvent, down: boolean) {
  if (!controlling.value || !event.code) return;
  // Tab out is the person's own way off the screen, so it is not taken.
  if (event.code === "Tab" && !event.ctrlKey && !event.altKey && !event.metaKey) return;
  event.preventDefault();
  r.send({ ...blank, kind: "key", code: event.code, down });
}
onBeforeUnmount(() => {
  if (frame) cancelAnimationFrame(frame);
});
</script>

<template>
  <!-- The surface over the screen that takes input while control is given. -->
  <div
    v-if="controlling"
    class="absolute inset-0 cursor-crosshair outline-none focus-visible:ring-2 focus-visible:ring-ring"
    tabindex="0"
    role="application"
    :aria-label="$ts('calls.control.surface', { name: who })"
    @pointermove="onMove"
    @pointerdown="onButton($event, true)"
    @pointerup="onButton($event, false)"
    @contextmenu.prevent
    @wheel="onWheel"
    @keydown="onKey($event, true)"
    @keyup="onKey($event, false)"
  />
  <div class="absolute right-1.5 bottom-1.5 flex max-w-[80%] items-center gap-1.5">
    <p
      v-if="outcome"
      class="rounded bg-black/70 px-1.5 py-0.5 text-[0.6875rem] text-white"
      role="status"
    >
      {{ outcome }}
    </p>
    <p
      v-if="controlling"
      class="rounded bg-black/70 px-1.5 py-0.5 text-[0.6875rem] text-white"
      role="status"
    >
      {{ $t("calls.control.controlling", { name: who }) }}
    </p>
    <Button
      v-if="controlling || state === 'asking'"
      size="small"
      variant="outline"
      class="h-6 shrink-0 bg-black/40 px-2 text-xs text-white"
      @click="r.release()"
    >
      {{ $t("calls.control.stop") }}
    </Button>
    <Button
      v-else
      size="small"
      variant="outline"
      class="h-6 shrink-0 bg-black/40 px-2 text-xs text-white"
      :title="$ts('calls.control.askHint', { name: who })"
      @click="r.ask()"
    >
      <MousePointer2 class="size-3" aria-hidden="true" />
      {{ $t("calls.control.ask") }}
    </Button>
  </div>
</template>
