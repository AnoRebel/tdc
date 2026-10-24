<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { isApiError, type EventDetail, type EventInvite } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import type { Picked } from "#layers/ui/app/lib/events";

/**
 * Inviting people and whole channels to an event, for the people running it.
 * Everybody invited is told; an invited channel's members can see an
 * invite-only event from then on.
 */
const props = defineProps<{ event: EventDetail }>();
const { $t, $ts } = useI18n();
const eventsApi = useEvents();

const invites = ref<EventInvite[]>([]);
const chosen = ref<Picked[]>([]);
const result = ref<string | null>(null);
const failed = ref<string | null>(null);
const sending = ref(false);

async function load() {
  try {
    invites.value = await eventsApi.invites(props.event.id);
  } catch {
    invites.value = [];
  }
}
onMounted(load);

async function send() {
  if (!chosen.value.length) return;
  sending.value = true;
  failed.value = null;
  try {
    const sent = await eventsApi.invite(props.event.id, {
      users: chosen.value.filter((each) => each.kind === "person").map((each) => each.id),
      channels: chosen.value.filter((each) => each.kind === "channel").map((each) => each.id),
    });
    result.value = $ts("events.invite.sent", { count: sent.invited });
    chosen.value = [];
    await load();
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("events.invite.couldNotSend");
  } finally {
    sending.value = false;
  }
}

async function withdraw(invite: EventInvite) {
  await eventsApi.uninvite(props.event.id, invite.id);
  await load();
}

/** What to call somebody invited by name. */
function inviteeName(invite: EventInvite): string {
  return invite.username
    ? nameOf({ username: invite.username, displayName: invite.displayName })
    : "";
}
</script>

<template>
  <section class="rounded-xl border p-4" aria-labelledby="invite-heading">
    <h2 id="invite-heading" class="text-sm font-semibold">{{ $t("events.invite.title") }}</h2>
    <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("events.invite.hint") }}</p>
    <div class="mt-3 flex items-end gap-2">
      <EventsPeoplePicker
        v-model="chosen"
        class="flex-1"
        channels
        :label="$ts('events.invite.who')"
      />
      <Button :disabled="!chosen.length || sending" @click="send">{{
        $t("events.invite.send")
      }}</Button>
    </div>
    <p v-if="result" class="mt-2 text-xs text-success" role="status">{{ result }}</p>
    <p v-if="failed" class="mt-2 text-xs text-destructive" role="alert">{{ failed }}</p>
    <ul v-if="invites.length" class="mt-3 flex flex-wrap gap-1.5">
      <li
        v-for="invite in invites"
        :key="invite.id"
        class="flex items-center gap-1 rounded-full bg-muted py-0.5 pr-0.5 pl-2 text-xs"
      >
        <Icon v-if="invite.channel" name="lucide:hash" class="size-3" aria-hidden="true" />
        {{ invite.channel || inviteeName(invite) }}
        <Button
          variant="ghost"
          size="icon"
          class="size-5"
          :label="$ts('events.invite.withdraw', { name: invite.channel || inviteeName(invite) })"
          @click="withdraw(invite)"
        >
          <Icon name="lucide:x" class="size-3" aria-hidden="true" />
        </Button>
      </li>
    </ul>
  </section>
</template>
