<script setup lang="ts">
import type { CallLinkTarget } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Clock, Lock, Mic, PhoneCall, Users, Video as VideoIcon } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";

/**
 * Where a call link opens.
 *
 * Says what the link leads to — the conversation's name and who is in the call,
 * nothing more — asks for the passcode where the link has one, and lets the
 * person in, or into the lobby to wait. A link to a call that has not started
 * waits for it here.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts } = useI18n();
const route = useRoute();
const api = useApi();
const c = useCall();

const secret = computed(() => String(route.params.secret));
const target = ref<CallLinkTarget | null>(null);
const unusable = ref<string | null>(null);
const passcode = ref("");
const refused = ref<string | null>(null);
const joining = ref(false);
const joinedRoom = ref<string | null>(null);

useHead({ title: () => target.value?.roomName || $ts("calls.linkPage.title") });

async function load() {
  try {
    target.value = await api.get<CallLinkTarget>(
      `/api/call-links/${encodeURIComponent(secret.value)}`,
    );
    unusable.value = null;
  } catch (cause) {
    target.value = null;
    unusable.value = isApiError(cause) ? cause.message : $ts("calls.couldNotJoin");
  }
}
await load();

// A call that has not started is waited for, without anybody pressing reload.
const poll = useIntervalFn(() => {
  if (target.value && !target.value.started) void load();
}, 5000);
onBeforeUnmount(() => poll.pause());

async function join(withVideo: boolean) {
  joining.value = true;
  refused.value = null;
  try {
    await c.joinByLink(secret.value, passcode.value, withVideo);
    if (!c.waitingIn.value) joinedRoom.value = c.roomId.value;
  } catch (cause) {
    refused.value = isApiError(cause) ? cause.message : $ts("calls.couldNotJoin");
  } finally {
    joining.value = false;
  }
}

const inCall = computed(
  () =>
    Boolean(joinedRoom.value && c.isIn(joinedRoom.value)) ||
    (c.joined.value && !c.waitingIn.value && joinedRoom.value !== null),
);
watch(
  () => c.waitingIn.value,
  (now, before) => {
    // Let in from the lobby: the call window takes over from here.
    if (before && !now && c.roomId.value) joinedRoom.value = c.roomId.value;
  },
);
</script>

<template>
  <div class="flex flex-1 items-center justify-center overflow-y-auto p-6">
    <div class="w-full max-w-sm space-y-5 rounded-xl border bg-background p-6 shadow-sm">
      <div v-if="unusable" class="space-y-2 text-center" role="alert">
        <PhoneCall class="mx-auto size-8 text-muted-foreground" aria-hidden="true" />
        <h1 class="text-base font-semibold">{{ $t("calls.linkPage.unusable") }}</h1>
        <p class="text-sm text-muted-foreground">{{ unusable }}</p>
      </div>

      <template v-else-if="target">
        <header class="space-y-1 text-center">
          <p class="text-xs text-muted-foreground">{{ $t("calls.linkPage.invited") }}</p>
          <h1 class="text-lg font-semibold tracking-tight">{{ target.roomName }}</h1>
        </header>

        <div class="rounded-lg bg-muted/50 p-3 text-sm">
          <p
            v-if="!target.started"
            class="flex items-center gap-2 text-muted-foreground"
            role="status"
          >
            <Clock class="size-4" aria-hidden="true" />
            {{ $t("calls.linkPage.notStarted") }}
          </p>
          <template v-else>
            <p class="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
              <Users class="size-3.5" aria-hidden="true" />
              {{ $t("calls.people", { count: target.people.length }) }}
            </p>
            <ul class="flex flex-wrap gap-2">
              <li
                v-for="name in target.people"
                :key="name"
                class="flex items-center gap-1.5 text-xs"
              >
                <Avatar :name="name" size="sm" />
                {{ name }}
              </li>
            </ul>
          </template>
        </div>

        <p v-if="inCall" class="text-center text-sm" role="status">
          {{ $t("calls.linkPage.inCall") }}
        </p>

        <div v-else-if="c.waitingIn.value" class="space-y-3 text-center" role="status">
          <template v-if="c.waitingIn.value.outcome === 'waiting'">
            <p class="text-sm">{{ $t("calls.linkPage.waiting") }}</p>
            <Button variant="ghost" size="small" @click="c.stopWaiting()">
              {{ $t("calls.linkPage.stopWaiting") }}
            </Button>
          </template>
          <p v-else class="text-sm text-destructive">{{ $t("calls.linkPage.turnedAway") }}</p>
        </div>

        <form v-else class="space-y-3" @submit.prevent="join(false)">
          <div v-if="target.protected" class="space-y-1">
            <label for="call-passcode" class="flex items-center gap-1.5 text-xs font-medium">
              <Lock class="size-3.5" aria-hidden="true" />
              {{ $t("calls.linkPage.passcode") }}
            </label>
            <Input
              id="call-passcode"
              v-model="passcode"
              type="password"
              autocomplete="off"
              :invalid="Boolean(refused)"
            />
          </div>
          <p v-if="target.lobby" class="text-xs text-muted-foreground">
            {{ $t("calls.linkPage.lobbyNote") }}
          </p>
          <p v-if="refused" class="text-sm text-destructive" role="alert">{{ refused }}</p>
          <div class="flex gap-2">
            <Button
              type="submit"
              variant="outline"
              class="flex-1"
              :disabled="joining || !target.started || (target.protected && !passcode)"
            >
              <Mic class="size-4" aria-hidden="true" />
              {{ $t("calls.joinAudio") }}
            </Button>
            <Button
              type="button"
              class="flex-1"
              :disabled="joining || !target.started || (target.protected && !passcode)"
              @click="join(true)"
            >
              <VideoIcon class="size-4" aria-hidden="true" />
              {{ $t("calls.joinVideo") }}
            </Button>
          </div>
        </form>
      </template>
    </div>
  </div>
</template>
