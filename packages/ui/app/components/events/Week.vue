<script setup lang="ts">
import type { CalendarEvent } from "@tdc/types";
import { format, parseISO } from "date-fns";
import { byDay, dayKey, viewerZone, weekOf } from "#layers/ui/app/lib/events";

/**
 * A week, a column a day, each listing what happens on it in order of time.
 * On a narrow screen the days stack, which reads as an agenda for the week.
 */
const props = defineProps<{ day: string; events: CalendarEvent[] }>();

const { $t, $localeRoute: localeRoute } = useI18n();
const days = computed(() => weekOf(props.day));
const grouped = computed(() => byDay(props.events, viewerZone()));
const today = dayKey(new Date());
</script>

<template>
  <ol class="grid gap-2 md:grid-cols-7">
    <li
      v-for="day in days"
      :key="day"
      class="min-h-32 rounded-xl border bg-card p-2"
      :class="day === today && 'border-primary/60'"
    >
      <p class="flex items-baseline gap-1.5 text-xs">
        <span class="font-medium">{{ format(parseISO(day), "EEE") }}</span>
        <span class="tabular text-muted-foreground">{{ format(parseISO(day), "d MMM") }}</span>
      </p>
      <ul v-if="grouped.get(day)?.length" class="mt-2 space-y-1.5">
        <li
          v-for="event in grouped.get(day)"
          :key="`${event.id}-${event.occurrenceStartsAt}`"
          class="rounded-lg border-l-2 bg-muted/30 py-1 pr-1 pl-2 text-xs"
          :class="
            event.cancelledAt
              ? 'border-muted-foreground/40 text-muted-foreground line-through'
              : 'border-primary'
          "
        >
          <NuxtLink
            :to="localeRoute({ name: 'events-id', params: { id: event.id } })"
            class="block hover:underline"
          >
            <span class="block truncate font-medium">{{ event.title }}</span>
            <EventsWhen
              class="text-[0.6875rem] text-muted-foreground"
              :occurrence="{
                startsAt: event.occurrenceStartsAt ?? event.startsAt,
                endsAt: event.occurrenceEndsAt ?? event.endsAt,
              }"
              :event="event"
              compact
            />
          </NuxtLink>
        </li>
      </ul>
      <p v-else class="mt-2 text-[0.6875rem] text-muted-foreground/70">
        {{ $t("events.nothingThisDay") }}
      </p>
    </li>
  </ol>
</template>
