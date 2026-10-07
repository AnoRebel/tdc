<script setup lang="ts">
import type { Call } from "@tdc/types";
import { ArrowLeftRight, Merge, PhoneOff } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The other calls somebody is in, held while they talk in this one: who is in
 * each, a switch to it, and a merge of its people into the call they are in
 * now — which makes it a conference. Switching holds the call they are in now.
 */

const { $t, $ts } = useI18n();
const { user } = useAuth();
const c = useCall();

function who(call: Call) {
  const names = call.participants
    .filter((each) => each.userId !== user.value?.id)
    .map((each) => each.username);
  return names.length ? names.join(", ") : $ts("calls.inProgress");
}
</script>

<template>
  <ul v-if="c.others.value.length" class="space-y-1" :aria-label="$ts('calls.heldCalls')">
    <li
      v-for="each in c.others.value"
      :key="each.id"
      class="flex items-center gap-2 rounded-md bg-muted/60 px-2 py-1 text-xs"
    >
      <span class="size-1.5 shrink-0 rounded-full bg-warning" aria-hidden="true" />
      <span class="min-w-0 flex-1 truncate">
        <span class="font-medium">{{ who(each) }}</span>
        <span class="text-muted-foreground"> · {{ $t("calls.onHold") }}</span>
      </span>
      <Button
        v-if="c.canMerge(each.roomId)"
        variant="ghost"
        size="small"
        class="h-6 px-2"
        :title="$ts('calls.mergeHint')"
        @click="c.merge(each.roomId)"
      >
        <Merge class="size-3" aria-hidden="true" />
        {{ $t("calls.merge") }}
      </Button>
      <Button variant="ghost" size="small" class="h-6 px-2" @click="c.switchTo(each.roomId)">
        <ArrowLeftRight class="size-3" aria-hidden="true" />
        {{ $t("calls.switch") }}
      </Button>
      <Button
        variant="ghost"
        size="icon"
        class="size-6"
        :label="$ts('calls.leaveHeld', { name: who(each) })"
        @click="c.leave(each.roomId)"
      >
        <PhoneOff class="size-3 text-destructive" />
      </Button>
    </li>
  </ul>
</template>
