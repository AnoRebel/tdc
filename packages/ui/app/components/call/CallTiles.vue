<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { MicOff } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";

/**
 * Everybody in the call, as tiles: their camera, or their name where they send
 * none, with their microphone state on every tile — what the others see of a
 * person is exactly what that person is sending.
 *
 * Every remote tile keeps its video element whether or not it shows video,
 * because that element is also what plays the person's audio.
 */

defineProps<{ compact?: boolean }>();

const { $t, $ts } = useI18n();
const { user } = useAuth();
const c = useCall();

const others = computed(() =>
  (c.call.value?.participants ?? []).filter((each) => each.userId !== user.value?.id),
);

function attach(element: unknown, stream: MediaStream | null | undefined) {
  if (element instanceof HTMLVideoElement && element.srcObject !== (stream ?? null)) {
    element.srcObject = stream ?? null;
  }
}
</script>

<template>
  <CallStage :compact="compact" />
  <ul
    class="grid gap-2"
    :class="
      compact
        ? 'grid-cols-2'
        : c.call.value?.presenter
          ? 'grid-cols-[repeat(auto-fill,minmax(9rem,1fr))]'
          : 'grid-cols-[repeat(auto-fit,minmax(14rem,1fr))] content-center'
    "
  >
    <li class="tile">
      <video
        v-if="c.camera.value"
        :ref="(el) => attach(el, c.local.value)"
        autoplay
        muted
        playsinline
        class="size-full -scale-x-100 object-cover"
      />
      <div v-else class="grid size-full place-items-center">
        <Avatar :name="user ? nameOf(user) : ''" :size="compact ? 'md' : 'lg'" />
      </div>
      <span class="tile-label">
        <MicOff v-if="!c.microphone.value" class="size-3" :aria-label="$ts('calls.muted')" />
        {{ $t("calls.you") }}
      </span>
    </li>
    <li v-for="person in others" :key="person.userId" class="tile">
      <video
        :ref="(el) => attach(el, c.remote.value.get(person.userId))"
        autoplay
        playsinline
        :muted="c.onHold.value"
        data-remote-video
        class="size-full object-cover"
        :class="!person.video && 'invisible absolute'"
      />
      <div v-if="!person.video" class="grid size-full place-items-center">
        <Avatar :name="nameOf(person)" :size="compact ? 'md' : 'lg'" />
      </div>
      <span class="tile-label">
        <MicOff v-if="!person.audio" class="size-3" :aria-label="$ts('calls.muted')" />
        <span v-if="person.handRaisedAt" :aria-label="$ts('calls.reactions.handUp')">✋</span>
        {{ nameOf(person) }}
        <span v-if="person.guest" class="rounded bg-white/20 px-1 text-[0.625rem] uppercase">
          {{ $t("calls.guest") }}
        </span>
        <span v-if="person.held" class="text-white/75">· {{ $t("calls.holding") }}</span>
      </span>
    </li>
  </ul>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

.tile {
  @apply relative aspect-video overflow-hidden rounded-lg bg-muted;
}
.tile-label {
  @apply absolute bottom-1.5 left-1.5 flex items-center gap-1 rounded bg-black/55 px-1.5 py-0.5 text-[0.6875rem] font-medium text-white;
}
</style>
