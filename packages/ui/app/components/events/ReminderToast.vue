<script setup lang="ts">
import type { EventReminderFrame } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * A reminder, arrived on the live connection, shown at the corner of
 * whichever page somebody is on. The notification is also in their list, and
 * on a device with the application closed it came by push; this is for the
 * person who is here.
 *
 * Listens for the window event the live connections raise rather than holding
 * a connection of its own.
 */
const { $t, $ts, $localeRoute: localeRoute } = useI18n();
const reminders = ref<EventReminderFrame[]>([]);

function arrived(event: Event) {
  const reminder = (event as CustomEvent<EventReminderFrame>).detail;
  const key = `${reminder.event}:${reminder.startsAt}:${reminder.minutes}`;
  if (reminders.value.some((each) => `${each.event}:${each.startsAt}:${each.minutes}` === key))
    return;
  reminders.value = [...reminders.value, reminder].slice(-3);
}
useEventListener(window, "tdc:event-reminder", arrived);

function dismiss(reminder: EventReminderFrame) {
  reminders.value = reminders.value.filter((each) => each !== reminder);
}

function lead(minutes: number): string {
  if (minutes >= 1440) return $ts("events.reminder.tomorrow");
  if (minutes >= 60) return $ts("events.reminder.hours", { count: Math.round(minutes / 60) });
  return $ts("events.reminder.minutes", { count: minutes });
}
</script>

<template>
  <div
    class="pointer-events-none fixed right-4 bottom-4 z-40 flex w-80 flex-col gap-2"
    aria-live="polite"
  >
    <div
      v-for="reminder in reminders"
      :key="`${reminder.event}-${reminder.minutes}`"
      class="motion-pop-in pointer-events-auto flex items-start gap-3 rounded-xl border bg-card p-3 shadow-lg"
      role="status"
    >
      <span class="grid size-8 shrink-0 place-items-center rounded-full bg-primary/10 text-primary">
        <Icon name="lucide:bell-ring" class="size-4" aria-hidden="true" />
      </span>
      <div class="min-w-0 flex-1">
        <p class="truncate text-sm font-medium">{{ reminder.title }}</p>
        <p class="text-xs text-muted-foreground">{{ lead(reminder.minutes) }}</p>
        <NuxtLink
          :to="localeRoute({ name: 'events-id', params: { id: reminder.event } })"
          class="mt-1 inline-block text-xs font-medium text-primary hover:underline"
          @click="dismiss(reminder)"
        >
          {{ reminder.online ? $t("events.reminder.openToJoin") : $t("events.reminder.open") }}
        </NuxtLink>
      </div>
      <Button
        variant="ghost"
        size="icon"
        class="size-6"
        :label="$ts('action.close')"
        @click="dismiss(reminder)"
      >
        <Icon name="lucide:x" class="size-3.5" aria-hidden="true" />
      </Button>
    </div>
  </div>
</template>
