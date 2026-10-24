<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import {
  Maximize2,
  Mic,
  MicOff,
  Minimize2,
  PictureInPicture2,
  PhoneOff,
  ChevronUp,
  ChevronDown,
  GripVertical,
} from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The call, wherever somebody is in the application.
 *
 * Mounted in the layout, so a call goes on while they read another
 * conversation, open a project or change a setting. Three sizes, chosen by the
 * person and kept while they move around: expanded over the page, docked as a
 * small window in a corner, or minimized to a bar with just the controls. And
 * picture-in-picture, which takes the call out of the page altogether so it
 * stays on top of other windows: the whole call where the platform can put a
 * document there, the main video where it can only do that.
 *
 */

const { $t, $ts, $localeRoute: localeRoute } = useI18n();
const c = useCall();

// The call's negotiation travels on the call host's connection.

const roomName = ref("");
watch(
  () => c.roomId.value,
  async (room) => {
    roomName.value = "";
    if (!room || c.guestOf.value.includes(room)) return;
    try {
      roomName.value = (await useRooms().get(room)).name;
    } catch {
      // The call still works without its conversation's name.
    }
  },
  { immediate: true },
);

/*
 * Picture-in-picture.
 *
 * Document picture-in-picture moves the whole call — tiles and controls — into
 * a small always-on-top window. Where a platform only has the older kind, the
 * first person's video goes there instead. Where it has neither, the control
 * is not offered.
 */
type DocumentPiP = {
  requestWindow: (options: { width: number; height: number }) => Promise<Window>;
};
const documentPiP = import.meta.client
  ? (window as Window & { documentPictureInPicture?: DocumentPiP }).documentPictureInPicture
  : undefined;
const videoPiP = import.meta.client && document.pictureInPictureEnabled;
const pipTarget = shallowRef<HTMLElement | null>(null);
const pipAvailable = Boolean(documentPiP) || Boolean(videoPiP);

async function pictureInPicture() {
  if (documentPiP) {
    const pip = await documentPiP.requestWindow({ width: 380, height: 300 });
    // The new window has none of our styles; it gets the same ones.
    for (const sheet of document.querySelectorAll("link[rel=stylesheet], style")) {
      pip.document.head.append(sheet.cloneNode(true));
    }
    pip.document.documentElement.className = document.documentElement.className;
    pip.document.body.className = "bg-background text-foreground";
    pipTarget.value = pip.document.body;
    pip.addEventListener("pagehide", () => {
      pipTarget.value = null;
    });
    return;
  }
  const video = document.querySelector<HTMLVideoElement>(
    "[data-call-window] video[data-remote-video]",
  );
  await video?.requestPictureInPicture().catch(() => {});
}

const inCall = computed(() => c.joined.value);
// Let into this call by a merge: its conversation is not theirs to open.
const guest = computed(() => Boolean(c.roomId.value && c.guestOf.value.includes(c.roomId.value)));

// A direct conversation has no name worth showing; the people in it are.
const { user: me } = useAuth();
const title = computed(() => {
  const others = (c.call.value?.participants ?? [])
    .filter((each) => each.userId !== me.value?.id)
    .map((each) => nameOf(each));
  if (roomName.value && !roomName.value.includes(",")) return roomName.value;
  return others.length ? others.join(", ") : roomName.value || $ts("calls.inProgress");
});

// The docked window and the minimized bar go wherever somebody puts them.
const floating = useTemplateRef<HTMLElement>("floating");
const grip = useTemplateRef<HTMLElement>("grip");
const {
  isDragging,
  nudge,
  style: floatingStyle,
  corner: floatingCorner,
} = useMovable(floating, grip, "call", "right-4 bottom-4");
</script>

<template>
  <div v-if="inCall" data-call-window>
    <!-- Out of the page, in its own small window. -->
    <Teleport v-if="pipTarget" :to="pipTarget">
      <div class="flex h-screen flex-col gap-2 p-2">
        <div class="min-h-0 flex-1 overflow-y-auto"><CallTiles compact /></div>
        <CallLobbyList />
        <CallHands />
        <CallHeld />
        <CallControls :room-name="roomName" in-picture />
      </div>
    </Teleport>

    <!-- Minimized: a bar with the controls and nothing else. -->
    <div
      v-else-if="c.view.value === 'minimized'"
      ref="floating"
      class="call-surface fixed z-40 flex items-center gap-2 rounded-full py-1.5 pr-3 pl-1"
      :class="[floatingCorner, isDragging && 'cursor-grabbing select-none']"
      :style="floatingStyle"
      role="region"
      :aria-label="$ts('calls.window')"
    >
      <span
        ref="grip"
        class="grip"
        role="button"
        tabindex="0"
        :aria-label="$ts('calls.move')"
        :title="$ts('calls.move')"
        @keydown="nudge"
      >
        <GripVertical class="size-3.5" aria-hidden="true" />
      </span>
      <span class="size-2 animate-pulse rounded-full bg-success" aria-hidden="true" />
      <NuxtLink
        v-if="c.roomId.value && !guest"
        :to="localeRoute({ name: 'rooms-id', params: { id: c.roomId.value } })"
        class="max-w-40 truncate text-xs font-medium hover:underline"
      >
        {{ title }}
      </NuxtLink>
      <span v-else class="max-w-40 truncate text-xs font-medium">{{ title }}</span>
      <span class="text-xs text-muted-foreground">
        {{
          c.onHold.value
            ? $t("calls.onHold")
            : $t("calls.people", { count: c.call.value?.participants.length ?? 1 })
        }}
      </span>
      <span
        v-if="c.others.value.length"
        class="rounded bg-warning/15 px-1 text-[0.6875rem] text-warning"
        :title="$ts('calls.heldCalls')"
      >
        {{ $t("calls.moreHeld", { count: c.others.value.length }) }}
      </span>
      <Button
        variant="ghost"
        size="icon"
        class="size-7"
        :label="c.microphone.value ? $ts('calls.mute') : $ts('calls.unmute')"
        :aria-pressed="!c.microphone.value"
        @click="c.toggleMicrophone()"
      >
        <Mic v-if="c.microphone.value" class="size-3.5" />
        <MicOff v-else class="size-3.5 text-destructive" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        class="size-7"
        :label="$ts('calls.restore')"
        @click="c.view.value = 'docked'"
      >
        <ChevronUp class="size-3.5" />
      </Button>
      <Button
        variant="danger"
        size="icon"
        class="size-7"
        :label="$ts('calls.leave')"
        @click="c.leave()"
      >
        <PhoneOff class="size-3.5" />
      </Button>
    </div>

    <!-- Docked or expanded. -->
    <div
      v-else-if="c.view.value === 'expanded'"
      class="call-surface fixed inset-3 z-40 flex flex-col gap-2 rounded-xl p-2 md:inset-6"
      role="region"
      :aria-label="$ts('calls.window')"
    >
      <CallReactionsFloat />
      <header class="flex items-center gap-1 px-1">
        <span class="size-2 rounded-full bg-success" aria-hidden="true" />
        <NuxtLink
          v-if="c.roomId.value && !guest"
          :to="localeRoute({ name: 'rooms-id', params: { id: c.roomId.value } })"
          class="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
        >
          {{ title }}
        </NuxtLink>
        <span v-else class="min-w-0 flex-1 truncate text-xs font-medium">{{ title }}</span>
        <Button
          v-if="pipAvailable"
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.pictureInPicture')"
          :title="$ts('calls.pictureInPicture')"
          @click="pictureInPicture()"
        >
          <PictureInPicture2 class="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.dock')"
          :title="$ts('calls.dock')"
          @click="c.view.value = 'docked'"
        >
          <Minimize2 class="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.minimize')"
          :title="$ts('calls.minimize')"
          @click="c.view.value = 'minimized'"
        >
          <ChevronDown class="size-3.5" />
        </Button>
      </header>
      <div class="min-h-0 flex-1 overflow-y-auto">
        <CallTiles />
      </div>
      <CallLobbyList />
      <CallHands />
      <CallHeld />
      <CallControls :room-name="roomName" />
    </div>

    <div
      v-else
      ref="floating"
      class="call-surface fixed z-40 flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2 rounded-xl p-2"
      :class="[floatingCorner, isDragging && 'cursor-grabbing select-none']"
      :style="floatingStyle"
      role="region"
      :aria-label="$ts('calls.window')"
    >
      <CallReactionsFloat />
      <header class="flex items-center gap-1 px-1">
        <span
          ref="grip"
          class="grip"
          role="button"
          tabindex="0"
          :aria-label="$ts('calls.move')"
          :title="$ts('calls.move')"
          @keydown="nudge"
        >
          <GripVertical class="size-3.5" aria-hidden="true" />
        </span>
        <span class="size-2 rounded-full bg-success" aria-hidden="true" />
        <NuxtLink
          v-if="c.roomId.value && !guest"
          :to="localeRoute({ name: 'rooms-id', params: { id: c.roomId.value } })"
          class="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
        >
          {{ title }}
        </NuxtLink>
        <span v-else class="min-w-0 flex-1 truncate text-xs font-medium">{{ title }}</span>
        <Button
          v-if="pipAvailable"
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.pictureInPicture')"
          :title="$ts('calls.pictureInPicture')"
          @click="pictureInPicture()"
        >
          <PictureInPicture2 class="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.expand')"
          :title="$ts('calls.expand')"
          @click="c.view.value = 'expanded'"
        >
          <Maximize2 class="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.minimize')"
          :title="$ts('calls.minimize')"
          @click="c.view.value = 'minimized'"
        >
          <ChevronDown class="size-3.5" />
        </Button>
      </header>
      <div class="min-h-0 flex-1 overflow-y-auto">
        <CallTiles compact />
      </div>
      <CallLobbyList />
      <CallHands />
      <CallHeld />
      <CallControls :room-name="roomName" />
    </div>
  </div>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

.call-surface {
  @apply border bg-background shadow-xl;
}
.grip {
  @apply grid size-7 shrink-0 cursor-grab place-items-center rounded text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring;
  touch-action: none;
}
</style>
