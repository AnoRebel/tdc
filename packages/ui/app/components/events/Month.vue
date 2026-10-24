<script setup lang="ts">
import type { CalendarEvent } from "@tdc/types";
import { format, parseISO } from "date-fns";
import { byDay, dayKey, describeWhen, monthGrid, viewerZone } from "#layers/ui/app/lib/events";

/**
 * A month, a week to a row, Monday first, each day listing what starts on it
 * in the reader's zone. A day with more than fits says how many more, which
 * opens the agenda for that day.
 */
const props = defineProps<{ month: string; events: CalendarEvent[] }>();
const emit = defineEmits<{ day: [day: string] }>();

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

const days = computed(() => monthGrid(props.month));
const grouped = computed(() => byDay(props.events, viewerZone()));
const today = dayKey(new Date());
const shown = 3;

const weekdayNames = computed(() =>
  days.value.slice(0, 7).map((day) => format(parseISO(day), "EEE")),
);

function timeOf(event: CalendarEvent): string {
  return (
    describeWhen(
      {
        startsAt: event.occurrenceStartsAt ?? event.startsAt,
        endsAt: event.occurrenceEndsAt ?? event.endsAt,
      },
      event,
    ).time.split(" – ")[0] ?? ""
  );
}
</script>

<template>
  <div class="overflow-hidden rounded-xl border bg-card">
    <div class="grid grid-cols-7 border-b bg-muted/40" aria-hidden="true">
      <span
        v-for="name in weekdayNames"
        :key="name"
        class="px-2 py-1.5 text-[0.6875rem] font-medium text-muted-foreground"
      >
        {{ name }}
      </span>
    </div>
    <ol class="grid grid-cols-7" :aria-label="$ts('events.views.month')">
      <li
        v-for="day in days"
        :key="day"
        class="min-h-24 border-r border-b p-1.5 [&:nth-child(7n)]:border-r-0"
        :class="!day.startsWith(props.month) && 'bg-muted/20 text-muted-foreground'"
      >
        <span
          class="tabular inline-grid size-6 place-items-center rounded-full text-xs"
          :class="day === today ? 'bg-primary font-semibold text-primary-foreground' : ''"
        >
          {{ Number(day.slice(8)) }}
        </span>
        <ul class="mt-1 space-y-0.5">
          <li
            v-for="event in (grouped.get(day) ?? []).slice(0, shown)"
            :key="`${event.id}-${event.occurrenceStartsAt}`"
          >
            <NuxtLink
              :to="localeRoute({ name: 'events-id', params: { id: event.id } })"
              class="block truncate rounded px-1 py-0.5 text-[0.6875rem] transition-colors hover:bg-accent"
              :class="
                event.cancelledAt
                  ? 'text-muted-foreground line-through'
                  : 'bg-primary/10 text-foreground'
              "
            >
              <span class="tabular text-muted-foreground">{{
                event.allDay ? "" : timeOf(event)
              }}</span>
              {{ event.title }}
            </NuxtLink>
          </li>
        </ul>
        <button
          v-if="(grouped.get(day)?.length ?? 0) > shown"
          type="button"
          class="mt-0.5 cursor-pointer px-1 text-[0.6875rem] text-muted-foreground hover:text-foreground"
          @click="emit('day', day)"
        >
          {{ $t("events.more", { count: (grouped.get(day)?.length ?? 0) - shown }) }}
        </button>
      </li>
    </ol>
  </div>
</template>
