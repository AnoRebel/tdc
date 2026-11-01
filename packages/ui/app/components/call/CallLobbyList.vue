<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { Check, X } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * People waiting to be let into the call, from a link with a lobby. Anybody in
 * the call who belongs to its conversation decides; somebody who came in by a
 * link sees nobody waiting, because the server will not let them decide.
 */

const { $t, $ts } = useI18n();
const c = useCall();
const guest = computed(() => Boolean(c.roomId.value && c.guestOf.value.includes(c.roomId.value)));
const waiting = computed(() =>
  guest.value || !c.call.value ? [] : (c.lobbies.value[c.call.value.id] ?? []),
);
</script>

<template>
  <section
    v-if="waiting.length"
    class="space-y-1 rounded-lg border border-primary/30 bg-primary/5 p-1.5"
    :aria-label="$ts('calls.lobby.title')"
  >
    <p class="px-1 text-xs font-medium" role="status">
      {{ $t("calls.lobby.waiting", { count: waiting.length }) }}
    </p>
    <div
      v-for="person in waiting"
      :key="person.userId"
      class="flex items-center gap-2 rounded px-1 py-0.5"
    >
      <Avatar :address="person.username" :name="nameOf(person)" size="sm" />
      <span class="min-w-0 flex-1 truncate text-xs">
        {{ nameOf(person) }}
      </span>
      <Button
        size="small"
        class="h-6 px-2 text-xs"
        :label="$ts('calls.lobby.admitWhom', { name: nameOf(person) })"
        @click="c.admit(person.userId, true)"
      >
        <Check class="size-3" aria-hidden="true" />
        {{ $t("calls.lobby.admit") }}
      </Button>
      <Button
        variant="ghost"
        size="icon"
        class="size-6"
        :label="$ts('calls.lobby.turnAwayWhom', { name: nameOf(person) })"
        @click="c.admit(person.userId, false)"
      >
        <X class="size-3" />
      </Button>
    </div>
  </section>
</template>
