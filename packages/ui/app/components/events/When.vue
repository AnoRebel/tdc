<script setup lang="ts">
import type { CalendarEvent } from "@tdc/types";
import { describeWhen } from "#layers/ui/app/lib/events";

/**
 * When an occurrence is, in the reader's own zone — and, beneath it, in the
 * organiser's when that is a different time, so nobody has to work out what
 * "18:00" meant to the person who wrote it.
 */
const props = defineProps<{
  occurrence: { startsAt: string; endsAt: string };
  event: Pick<CalendarEvent, "timeZone" | "allDay">;
  /** One line, for a card; otherwise the day and the time on two. */
  compact?: boolean;
}>();

const { $t } = useI18n();
const when = computed(() => describeWhen(props.occurrence, props.event));
</script>

<template>
  <span v-if="props.compact" class="inline-flex flex-wrap items-baseline gap-x-1.5">
    <span class="tabular">{{ when.time || $t("events.allDay") }}</span>
    <span v-if="when.theirs" class="text-muted-foreground">· {{ when.theirs }}</span>
  </span>
  <span v-else class="block">
    <time :datetime="props.occurrence.startsAt" class="block font-medium">{{ when.day }}</time>
    <span class="tabular block text-muted-foreground">{{ when.time || $t("events.allDay") }}</span>
    <span v-if="when.theirs" class="block text-xs text-muted-foreground">
      {{ $t("events.organisersTime", { time: when.theirs }) }}
    </span>
  </span>
</template>
