<script setup lang="ts">
import { MonitorX } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The shared screen, above everybody's tiles, while somebody presents.
 *
 * One presenter at a time, decided by the server. The element that shows the
 * screen also plays its sound — except for our own share, which would echo.
 * A moderator can take the presenter off the stage; the server checks they
 * may.
 */

defineProps<{ compact?: boolean }>();
const { $t, $ts } = useI18n();
const { user, can } = useAuth();
const c = useCall();

const presenter = computed(() => c.call.value?.presenter ?? null);
const mine = computed(() => Boolean(presenter.value && presenter.value === user.value?.id));
const who = computed(
  () => c.call.value?.participants.find((each) => each.userId === presenter.value)?.username ?? "",
);
const stream = computed(() =>
  mine.value
    ? c.screen.value
    : presenter.value
      ? (c.remoteScreen.value.get(presenter.value) ?? null)
      : null,
);

function attach(element: unknown, media: MediaStream | null) {
  if (element instanceof HTMLVideoElement && element.srcObject !== media) element.srcObject = media;
}
</script>

<template>
  <figure
    v-if="presenter"
    class="relative overflow-hidden rounded-lg bg-black"
    :class="compact ? 'mb-2 aspect-video' : 'mx-auto mb-2 aspect-video max-h-[65vh]'"
    data-call-stage
  >
    <video
      :ref="(el) => attach(el, stream)"
      autoplay
      playsinline
      :muted="mine || c.onHold.value"
      class="size-full object-contain"
      :aria-label="$ts('calls.stage.screenOf', { name: mine ? $ts('calls.you') : who })"
    />
    <figcaption
      class="absolute top-1.5 left-1.5 flex items-center gap-1.5 rounded bg-black/60 px-1.5 py-0.5 text-[0.6875rem] font-medium text-white"
    >
      {{ mine ? $t("calls.stage.youPresent") : $t("calls.stage.presenting", { name: who }) }}
    </figcaption>
    <Button
      v-if="mine"
      size="small"
      variant="danger"
      class="absolute top-1.5 right-1.5 h-6 px-2 text-xs"
      @click="c.stopSharing()"
    >
      <MonitorX class="size-3" aria-hidden="true" />
      {{ $t("calls.stage.stop") }}
    </Button>
    <Button
      v-else-if="can('moderation.restrict')"
      size="small"
      variant="outline"
      class="absolute top-1.5 right-1.5 h-6 bg-black/40 px-2 text-xs text-white"
      @click="c.clearStage()"
    >
      <MonitorX class="size-3" aria-hidden="true" />
      {{ $t("calls.stage.clear") }}
    </Button>
  </figure>
</template>
