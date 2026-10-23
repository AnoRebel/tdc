<script setup lang="ts">
import { shownName } from "#layers/ui/app/lib/call-names";
import { Hand, X } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Hands up, in the order they went up. Whoever presents, and a moderator,
 * can lower them — taking the question, or moving on; everybody else sees
 * the queue.
 */

const { $t, $ts } = useI18n();
const { user, can } = useAuth();
const c = useCall();
const mayLower = computed(
  () => c.call.value?.presenter === user.value?.id || can("moderation.restrict"),
);
</script>

<template>
  <section
    v-if="c.hands.value.length"
    class="rounded-lg border border-warning/30 bg-warning/5 p-1.5 text-xs"
    :aria-label="$ts('calls.reactions.hands')"
  >
    <p class="flex items-center gap-1 px-1 font-medium">
      <Hand class="size-3.5 text-warning" aria-hidden="true" />
      {{ $t("calls.reactions.handsUp", { count: c.hands.value.length }) }}
    </p>
    <ol class="mt-1 space-y-0.5">
      <li
        v-for="(person, index) in c.hands.value"
        :key="person.userId"
        class="flex items-center gap-2 rounded px-1"
      >
        <span class="w-4 text-right text-muted-foreground tabular-nums">{{ index + 1 }}</span>
        <span class="min-w-0 flex-1 truncate">{{ shownName(person) }}</span>
        <Button
          v-if="mayLower && person.userId !== user?.id"
          variant="ghost"
          size="icon"
          class="size-6"
          :label="$ts('calls.reactions.lowerWhose', { name: shownName(person) })"
          @click="c.setHand(false, person.userId)"
        >
          <X class="size-3" />
        </Button>
      </li>
    </ol>
  </section>
</template>
