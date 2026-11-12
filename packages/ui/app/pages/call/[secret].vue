<script setup lang="ts">
import type { CallLinkTarget, CallLinkUse } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";
import { useIntervalFn } from "@vueuse/core";
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

// Its own layout and no sign-in requirement: somebody without an account may
// join as a guest where the link and the server allow it.
definePageMeta({ layout: "call" });

const { $t, $ts } = useI18n();
const route = useRoute();
const api = useApi();
const c = useCall();
const auth = useAuth();
const { $localeRoute: localeRoute } = useI18n();

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

/*
 * Joining without an account: a name to be shown by, and the passcode if the
 * link has one. The server gives a guest account for this call alone, and its
 * session; then it is the same as anybody using the link.
 */
const guestRules = v.object({
  name: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("calls.linkPage.guestNameNeeded")),
    v.maxLength(64),
  ),
  passcode: v.string(),
});
const guestForm = useForm({
  defaultValues: { name: "", passcode: "" },
  validators: { onSubmit: guestRules },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      const answer = await api.post<{ use: CallLinkUse }>(
        `/api/call-links/${encodeURIComponent(secret.value)}/guest`,
        { name: value.name.trim(), passcode: value.passcode },
      );
      await auth.refresh();
      if (answer.use.waiting) {
        c.waitingIn.value = {
          callId: answer.use.callId,
          roomId: answer.use.roomId,
          video: false,
          outcome: "waiting",
        };
      } else {
        await c.join(answer.use.roomId, false, { asGuestOf: answer.use.callId });
        joinedRoom.value = c.roomId.value;
      }
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("calls.couldNotJoin");
    }
  },
});
const guestErrors = guestForm.useStore((state) => state.errors);

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

// A call that has not started is waited for, without anybody pressing reload.
const poll = useIntervalFn(() => {
  if (target.value && !target.value.started) void load();
}, 5000);
onBeforeUnmount(() => poll.pause());

if (!auth.ready.value) await auth.refresh();
await load();
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

        <p
          v-else-if="joinedRoom && !c.joined.value && auth.user.value?.guest"
          class="text-center text-sm"
          role="status"
        >
          {{ $t("calls.linkPage.ended") }}
        </p>

        <div v-else-if="!auth.signedIn.value" class="space-y-4">
          <NuxtLink
            :to="localeRoute({ name: 'signin', query: { next: route.fullPath } })"
            class="btn btn-primary h-9 w-full"
          >
            {{ $t("calls.linkPage.signIn") }}
          </NuxtLink>
          <form
            v-if="target.guests"
            class="space-y-3 border-t pt-4"
            @submit.prevent="guestForm.handleSubmit()"
          >
            <p class="text-xs text-muted-foreground">{{ $t("calls.linkPage.guestNote") }}</p>
            <guestForm.Field name="name">
              <template #default="{ field }">
                <div class="space-y-1">
                  <label for="guest-name" class="text-xs font-medium">
                    {{ $t("calls.linkPage.guestName") }}
                  </label>
                  <Input
                    id="guest-name"
                    :model-value="field.state.value"
                    autocomplete="name"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </div>
              </template>
            </guestForm.Field>
            <guestForm.Field v-if="target.protected" name="passcode">
              <template #default="{ field }">
                <div class="space-y-1">
                  <label for="guest-passcode" class="flex items-center gap-1.5 text-xs font-medium">
                    <Lock class="size-3.5" aria-hidden="true" />
                    {{ $t("calls.linkPage.passcode") }}
                  </label>
                  <Input
                    id="guest-passcode"
                    type="password"
                    autocomplete="off"
                    :model-value="field.state.value"
                    @update:model-value="field.handleChange($event)"
                  />
                </div>
              </template>
            </guestForm.Field>
            <p v-if="refused || guestErrors.length" class="text-sm text-destructive" role="alert">
              {{ refused || $t("calls.linkPage.guestNameNeeded") }}
            </p>
            <Button
              type="submit"
              variant="outline"
              class="w-full"
              :disabled="guestForm.state.isSubmitting || !target.started"
            >
              <Mic class="size-4" aria-hidden="true" />
              {{ $t("calls.linkPage.joinAsGuest") }}
            </Button>
          </form>
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
