<script setup lang="ts">
import type { CallRing } from "@tdc/types";
import {
  BellOff,
  GripVertical,
  MessageSquare,
  Mic,
  Pause,
  PhoneOff,
  Send,
  Video,
  Volume2,
  VolumeX,
} from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Somebody is calling — or several people are.
 *
 * A small window, not a dialog: nothing behind it is blocked, so somebody in
 * the middle of writing can finish the sentence first. It can be dragged out of
 * the way, or moved from the keyboard, and stays where it is put. It offers to
 * answer by voice or with video, or to decline, and it stops — here and on every
 * other device — the moment any one of them answers or declines.
 *
 * Every call ringing at once is listed, newest first, each with its own answer
 * and decline, so nobody is answered for the person. While they are already in
 * a call it says so, and offers both things a phone does: hold that call and
 * answer, or end it and answer.
 *
 * Declining can carry a few words for the caller — a quick reply or the
 * person's own — and ending a call to take another can carry a few for the
 * people left in it. Either is posted in its conversation, where it stays.
 *
 * The ring is a tone made here rather than a sound file, so it costs nothing to
 * download; it can be silenced without declining.
 */

const props = defineProps<{ rings: CallRing[] }>();
const { $t, $ts } = useI18n();
const c = useCall();

const surface = useTemplateRef<HTMLElement>("surface");
const grip = useTemplateRef<HTMLElement>("grip");
const { isDragging, nudge, style, corner } = useMovable(surface, grip, "ring", "top-4 right-4");

/*
 * The ring: the tone chosen for the newest caller, or the default, unless the
 * ringer is muted on this device or this ring was silenced. Muting is kept;
 * silencing lasts only for these calls.
 */
const ringtones = useRingtones();
const silenced = ref(false);
let stop: (() => void) | null = null;
const newest = computed(() => props.rings.at(-1));
watchEffect(() => {
  stop?.();
  stop = null;
  if (!newest.value || silenced.value) return;
  stop = ringtones.ring(newest.value.from.address);
});
onBeforeUnmount(() => stop?.());

const newestFirst = computed(() => [...props.rings].reverse());

/** The ring a reply is being written for, and the words. */
const replyingTo = ref<string | null>(null);
const reply = ref("");
const quickReplies = computed(() => [
  $ts("calls.quick.busy"),
  $ts("calls.quick.later"),
  $ts("calls.quick.meeting"),
  $ts("calls.quick.write"),
]);
function declineSaying(ring: CallRing, words: string) {
  replyingTo.value = null;
  reply.value = "";
  void c.decline(ring, words);
}

/** Words for the people of the call being ended to take another. */
const farewell = ref("");
const farewells = computed(() => [$ts("calls.farewell.other"), $ts("calls.farewell.back")]);
// In a call, with a ring that would hold or end it; an invitation to be merged
// asks neither.
const busy = computed(() => c.joined.value && props.rings.some((ring) => !ring.conference));
function where(ring: CallRing) {
  if (ring.conference) return $ts("calls.conferenceInvite");
  return ring.kind === "direct" ? $ts("calls.directCall") : `#${ring.roomName}`;
}
</script>

<template>
  <div
    ref="surface"
    class="fixed z-50 w-[min(22rem,calc(100vw-2rem))] rounded-xl border bg-background p-2 shadow-xl"
    :class="[corner, isDragging && 'cursor-grabbing select-none']"
    :style="style"
    role="alertdialog"
    aria-modal="false"
    :aria-label="$ts('calls.incoming', { name: newestFirst[0]?.from.username ?? '' })"
    data-incoming-call
  >
    <div class="flex items-center gap-1 px-1 pb-1">
      <span
        ref="grip"
        class="grid size-7 shrink-0 cursor-grab place-items-center rounded text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
        style="touch-action: none"
        role="button"
        tabindex="0"
        :aria-label="$ts('calls.move')"
        :title="$ts('calls.move')"
        @keydown="nudge"
      >
        <GripVertical class="size-3.5" aria-hidden="true" />
      </span>
      <span class="flex-1 text-xs font-medium text-muted-foreground">
        {{
          rings.length > 1
            ? $t("calls.incomingMany", { count: rings.length })
            : $t("calls.incomingOne")
        }}
      </span>
      <Button
        variant="ghost"
        size="icon"
        class="size-7"
        :label="$ts('calls.silence')"
        :title="$ts('calls.silence')"
        :aria-pressed="silenced"
        @click="silenced = true"
      >
        <BellOff class="size-3.5" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        class="size-7"
        :label="
          ringtones.settings.value.muted ? $ts('calls.unmuteRinger') : $ts('calls.muteRinger')
        "
        :title="
          ringtones.settings.value.muted ? $ts('calls.unmuteRinger') : $ts('calls.muteRinger')
        "
        :aria-pressed="ringtones.settings.value.muted"
        @click="ringtones.setMuted(!ringtones.settings.value.muted)"
      >
        <VolumeX v-if="ringtones.settings.value.muted" class="size-3.5" />
        <Volume2 v-else class="size-3.5" />
      </Button>
    </div>

    <p
      v-if="busy"
      class="mx-1 mb-1.5 rounded-md bg-warning/10 px-2 py-1 text-xs text-warning"
      role="status"
    >
      {{ $t("calls.whileInCall") }}
    </p>
    <label v-if="busy" class="mx-1 mb-1.5 flex items-center gap-1.5 text-xs">
      <span class="shrink-0 text-muted-foreground">{{ $t("calls.farewellLabel") }}</span>
      <select v-model="farewell" class="h-7 min-w-0 flex-1 rounded-md border bg-background px-1">
        <option value="">{{ $t("calls.farewellNone") }}</option>
        <option v-for="words in farewells" :key="words" :value="words">{{ words }}</option>
      </select>
    </label>

    <ul class="max-h-80 space-y-1.5 overflow-y-auto">
      <li v-for="ring in newestFirst" :key="ring.call" class="rounded-lg border bg-background p-2">
        <div class="flex items-center gap-2.5">
          <Avatar :name="ring.from.username" size="md" class="ring-pulse" />
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-semibold">{{ ring.from.username }}</p>
            <p v-if="ring.conference" class="text-xs text-muted-foreground">
              {{ where(ring) }}
            </p>
            <p v-else class="truncate text-xs text-muted-foreground">
              {{ $t("calls.isCalling") }} · {{ where(ring) }}
            </p>
          </div>
          <select
            class="h-7 max-w-24 rounded-md border bg-background px-1 text-xs"
            :value="ringtones.settings.value.byPerson[ring.from.address] ?? ''"
            :aria-label="$ts('calls.toneForPerson', { name: ring.from.username })"
            :title="$ts('calls.toneForPerson', { name: ring.from.username })"
            @change="
              ringtones.setPersonTone(
                ring.from.address,
                ($event.target as HTMLSelectElement).value || null,
              )
            "
          >
            <option value="">{{ $t("calls.defaultToneShort") }}</option>
            <option v-for="tone in ringtones.tones.value" :key="tone.id" :value="tone.id">
              {{ tone.name }}
            </option>
          </select>
        </div>
        <div class="mt-2 flex flex-wrap gap-1.5">
          <Button variant="danger" size="small" class="flex-1" @click="c.decline(ring)">
            <PhoneOff class="size-3.5" aria-hidden="true" />
            {{ $t("calls.decline") }}
          </Button>
          <Button
            v-if="!ring.conference"
            variant="ghost"
            size="icon"
            class="size-8 shrink-0"
            :label="$ts('calls.declineWithMessage')"
            :title="$ts('calls.declineWithMessage')"
            :aria-expanded="replyingTo === ring.call"
            @click="replyingTo = replyingTo === ring.call ? null : ring.call"
          >
            <MessageSquare class="size-3.5" />
          </Button>
          <!-- Being merged: the call shared with whoever merges is left for
               the conference, so there is nothing to hold or end. -->
          <Button v-if="ring.conference" size="small" class="flex-1" @click="c.answer(ring, false)">
            <Mic class="size-3.5" aria-hidden="true" />
            {{ $t("calls.joinConference") }}
          </Button>
          <div v-else-if="busy" class="flex basis-full gap-1.5">
            <Button variant="outline" size="small" class="flex-1" @click="c.answer(ring, false)">
              <Pause class="size-3.5" aria-hidden="true" />
              {{ $t("calls.holdAndAnswer") }}
            </Button>
            <Button
              size="small"
              class="flex-1"
              @click="c.answer(ring, false, { endCurrent: true, farewell })"
            >
              <PhoneOff class="size-3.5" aria-hidden="true" />
              {{ $t("calls.endAndAnswer") }}
            </Button>
          </div>
          <template v-else>
            <Button variant="outline" size="small" class="flex-1" @click="c.answer(ring, false)">
              <Mic class="size-3.5" aria-hidden="true" />
              {{ $t("calls.answer") }}
            </Button>
            <Button size="small" class="flex-1" @click="c.answer(ring, true)">
              <Video class="size-3.5" aria-hidden="true" />
              {{ $t("calls.answerVideo") }}
            </Button>
          </template>
        </div>
        <div v-if="replyingTo === ring.call" class="mt-2 space-y-1.5">
          <div class="flex flex-wrap gap-1">
            <button
              v-for="words in quickReplies"
              :key="words"
              type="button"
              class="rounded-full border px-2 py-0.5 text-xs transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
              @click="declineSaying(ring, words)"
            >
              {{ words }}
            </button>
          </div>
          <form class="flex gap-1" @submit.prevent="declineSaying(ring, reply)">
            <input
              v-model="reply"
              maxlength="280"
              class="h-7 min-w-0 flex-1 rounded-md border bg-background px-2 text-xs"
              :placeholder="$ts('calls.ownWords')"
              :aria-label="$ts('calls.ownWords')"
            />
            <Button
              type="submit"
              size="icon"
              class="size-7"
              :disabled="!reply.trim()"
              :label="$ts('calls.declineAndSend')"
              :title="$ts('calls.declineAndSend')"
            >
              <Send class="size-3.5" />
            </Button>
          </form>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

:root[data-motion="full"] .ring-pulse {
  animation: ring-pulse 1.6s ease-out infinite;
}
@keyframes ring-pulse {
  0% {
    box-shadow: 0 0 0 0 color-mix(in oklab, var(--color-success) 55%, transparent);
  }
  100% {
    box-shadow: 0 0 0 12px transparent;
  }
}
</style>
