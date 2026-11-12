<script setup lang="ts">
import type { CalendarEvent } from "@tdc/types";
import { addDays } from "date-fns";

/**
 * A channel's coming events, as one line under its header: the next one by
 * name and time, and how many more, which open the Events page filtered to
 * the channel. Absent when nothing is coming.
 */
const props = defineProps<{ channelId: string }>();
const { $t, $localeRoute: localeRoute } = useI18n();
const eventsApi = useEvents();

const coming = ref<CalendarEvent[]>([]);
async function load() {
  try {
    const now = new Date();
    coming.value = (
      await eventsApi.list({
        from: now.toISOString(),
        to: addDays(now, 60).toISOString(),
        channel: props.channelId,
        limit: 20,
      })
    ).filter((event) => !event.cancelledAt);
  } catch {
    coming.value = [];
  }
}
watch(
  () => props.channelId,
  () => void load(),
  { immediate: true },
);

const next = computed(() => coming.value[0]);
</script>

<template>
  <div
    v-if="next"
    class="flex shrink-0 items-center gap-2 border-b bg-muted/20 px-4 py-1.5 text-xs"
  >
    <Icon name="lucide:calendar-days" class="size-3.5 shrink-0 text-primary" aria-hidden="true" />
    <NuxtLink
      :to="localeRoute({ name: 'events-id', params: { id: next.id } })"
      class="flex min-w-0 items-center gap-1.5 hover:underline"
    >
      <span class="truncate font-medium">{{ next.title }}</span>
      <EventsWhen
        class="shrink-0 text-muted-foreground"
        :occurrence="{
          startsAt: next.occurrenceStartsAt ?? next.startsAt,
          endsAt: next.occurrenceEndsAt ?? next.endsAt,
        }"
        :event="next"
        compact
      />
    </NuxtLink>
    <NuxtLink
      v-if="coming.length > 1"
      :to="localeRoute({ name: 'events', query: { channel: props.channelId } })"
      class="ml-auto shrink-0 text-muted-foreground hover:text-foreground"
    >
      {{ $t("events.moreInChannel", { count: coming.length - 1 }) }}
    </NuxtLink>
  </div>
</template>
