<script setup lang="ts">
import type { CalendarEvent } from "@tdc/types";
import { tz } from "@date-fns/tz";
import { format, parseISO } from "date-fns";
import { viewerZone } from "#layers/ui/app/lib/events";

/**
 * One occurrence of an event in a list: its day on the left, what and when in
 * the middle, and the person's own answer on the right.
 */
const props = defineProps<{ event: CalendarEvent }>();

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

const start = computed(() => props.event.occurrenceStartsAt ?? props.event.startsAt);
const end = computed(() => props.event.occurrenceEndsAt ?? props.event.endsAt);
const zone = computed(() => (props.event.allDay ? props.event.timeZone : viewerZone()));
const dayNumber = computed(() => format(parseISO(start.value), "d", { in: tz(zone.value) }));
const monthName = computed(() => format(parseISO(start.value), "MMM", { in: tz(zone.value) }));
const cancelled = computed(() => Boolean(props.event.cancelledAt));
</script>

<template>
  <NuxtLink
    :to="localeRoute({ name: 'events-id', params: { id: props.event.id } })"
    class="motion-row group flex items-center gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-accent"
    :aria-label="`${props.event.title}, ${dayNumber} ${monthName}`"
  >
    <span
      class="grid w-11 shrink-0 place-items-center rounded-md border bg-surface py-1 leading-none"
      aria-hidden="true"
    >
      <span class="text-[0.625rem] font-semibold tracking-wide text-primary uppercase">{{
        monthName
      }}</span>
      <span class="tabular mt-0.5 text-base font-semibold">{{ dayNumber }}</span>
    </span>

    <span class="min-w-0 flex-1">
      <span class="flex items-center gap-2">
        <span
          class="truncate text-sm font-medium"
          :class="cancelled && 'text-muted-foreground line-through'"
        >
          {{ props.event.title }}
        </span>
        <span
          class="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground"
        >
          {{ $t(`events.kinds.${props.event.kind}`) }}
        </span>
        <span
          v-if="props.event.recurrence"
          class="shrink-0 text-muted-foreground"
          :title="$ts('events.repeats')"
        >
          <Icon name="lucide:repeat" class="size-3" :aria-label="$ts('events.repeats')" />
        </span>
      </span>
      <span
        class="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground"
      >
        <EventsWhen :occurrence="{ startsAt: start, endsAt: end }" :event="props.event" compact />
        <span v-if="props.event.channel" class="inline-flex items-center gap-1">
          <Icon name="lucide:hash" class="size-3" aria-hidden="true" />{{
            props.event.channel.name
          }}
        </span>
        <span v-if="props.event.online" class="inline-flex items-center gap-1">
          <Icon name="lucide:video" class="size-3" aria-hidden="true" />{{
            $t("events.onlineHere")
          }}
        </span>
        <span v-else-if="props.event.location" class="inline-flex min-w-0 items-center gap-1">
          <Icon name="lucide:map-pin" class="size-3 shrink-0" aria-hidden="true" />
          <span class="truncate">{{ props.event.location }}</span>
        </span>
      </span>
    </span>

    <span
      v-if="cancelled"
      class="shrink-0 rounded-full bg-destructive/10 px-2 py-0.5 text-[0.6875rem] font-medium text-destructive"
    >
      {{ $t("events.cancelled") }}
    </span>
    <span
      v-else-if="props.event.myAnswer && props.event.myAnswer !== 'declined'"
      class="shrink-0 rounded-full px-2 py-0.5 text-[0.6875rem] font-medium"
      :class="
        props.event.myAnswer === 'going'
          ? 'bg-success/15 text-success'
          : 'bg-muted text-muted-foreground'
      "
    >
      {{ $t(`events.answers.${props.event.myAnswer}`) }}
    </span>
  </NuxtLink>
</template>
