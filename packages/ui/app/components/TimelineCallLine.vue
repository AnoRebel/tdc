<script setup lang="ts">
import type { Message } from "@tdc/types";
import { Circle, Phone, PhoneOff, Square } from "lucide-vue-next";
import { atTime } from "#layers/ui/app/lib/dates";
import { nameAt } from "#layers/ui/app/lib/names";

/**
 * A call starting or ending, or its recording, as a line in the conversation
 * between the messages around it — who and when, and for an end how long.
 */
const props = defineProps<{ line: Message }>();
const { $t, $ts } = useI18n();

const who = computed(() => props.line.senderName || nameAt(props.line.sender));
const seconds = computed(() => (props.line.content as { seconds?: number }).seconds ?? 0);
const lasted = computed(() => {
  const minutes = Math.round(seconds.value / 60);
  if (minutes < 1) return $ts("calls.line.underMinute");
  if (minutes < 60) return $ts("calls.line.minutes", { minutes });
  return $ts("calls.line.hours", { hours: Math.floor(minutes / 60), minutes: minutes % 60 });
});
</script>

<template>
  <p class="flex items-center justify-center gap-2 py-1 text-xs text-muted-foreground" role="note">
    <Phone v-if="line.type === 'call.started'" class="size-3.5 text-success" aria-hidden="true" />
    <PhoneOff v-else-if="line.type === 'call.ended'" class="size-3.5" aria-hidden="true" />
    <Circle
      v-else-if="line.type === 'call.recording_started'"
      class="size-3.5 text-destructive"
      aria-hidden="true"
    />
    <Square v-else class="size-3.5" aria-hidden="true" />
    <span>
      <template v-if="line.type === 'call.started'">
        {{ $t("calls.line.started", { name: who }) }}
      </template>
      <template v-else-if="line.type === 'call.ended'">
        {{ $t("calls.line.ended", { duration: lasted }) }}
      </template>
      <template v-else-if="line.type === 'call.recording_started'">
        {{ $t("calls.line.recordingStarted", { name: who }) }}
      </template>
      <template v-else>
        {{ $t("calls.line.recordingStopped", { name: who, duration: lasted }) }}
      </template>
    </span>
    <time :datetime="line.sentAt">{{ atTime(line.sentAt) }}</time>
  </p>
</template>
