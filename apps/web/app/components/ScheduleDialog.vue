<script setup lang="ts">
import { Clock } from "lucide-vue-next";

/**
 * Choosing when to send a message.
 *
 * Presets first, because "tomorrow morning" is what somebody writing outside
 * office hours actually wants, and making them work out the date and time for
 * that is the reason scheduled sending goes unused.
 */

defineProps<{ body: string }>();

const emit = defineEmits<{ cancel: []; schedule: [sendAt: Date] }>();

/** The next occurrence of a given hour, today if it has not passed. */
function at(hour: number, addDays = 0): Date {
  const when = new Date();
  when.setDate(when.getDate() + addDays);
  when.setHours(hour, 0, 0, 0);
  return when;
}

const presets = computed(() => {
  const now = new Date();
  const options: { label: string; when: Date }[] = [];

  // Later today, only while there is still a later today to speak of.
  if (now.getHours() < 17) options.push({ label: "This evening, 6pm", when: at(18) });

  options.push({ label: "Tomorrow morning, 9am", when: at(9, 1) });
  options.push({ label: "Tomorrow evening, 6pm", when: at(18, 1) });

  // The coming Monday, for anything that should wait for the working week.
  const monday = new Date();
  monday.setDate(monday.getDate() + ((8 - monday.getDay()) % 7 || 7));
  monday.setHours(9, 0, 0, 0);
  options.push({ label: "Monday morning, 9am", when: monday });

  return options;
});

/** A custom time, in the browser's own local format. */
const custom = ref("");

const customDate = computed(() => (custom.value ? new Date(custom.value) : null));
const customValid = computed(
  () => !!customDate.value && customDate.value.getTime() > Date.now() + 10_000,
);

/** The earliest a picker should offer, matching what the server accepts. */
const earliest = computed(() => {
  const soon = new Date(Date.now() + 60_000);
  // datetime-local wants the local time without a zone, which toISOString
  // will not give: it converts to UTC and would offer the wrong minute.
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    `${soon.getFullYear()}-${pad(soon.getMonth() + 1)}-${pad(soon.getDate())}` +
    `T${pad(soon.getHours())}:${pad(soon.getMinutes())}`
  );
});

function describe(when: Date): string {
  return when.toLocaleString([], {
    weekday: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    aria-label="Send later"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div class="w-[min(24rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <Clock class="size-4" />
        Send later
      </h2>

      <p
        class="mt-3 max-h-16 overflow-y-auto rounded-lg bg-muted/50 px-3 py-2 text-xs text-muted-foreground"
      >
        {{ body }}
      </p>

      <div class="mt-4 space-y-1">
        <button
          v-for="preset in presets"
          :key="preset.label"
          type="button"
          class="flex w-full cursor-pointer items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-accent"
          @click="emit('schedule', preset.when)"
        >
          <span>{{ preset.label }}</span>
          <span class="text-xs text-muted-foreground">{{ describe(preset.when) }}</span>
        </button>
      </div>

      <div class="mt-4 border-t border-border pt-3">
        <label for="schedule-custom" class="mb-1 block text-xs font-medium"> Or pick a time </label>
        <input
          id="schedule-custom"
          v-model="custom"
          type="datetime-local"
          class="input h-9"
          :min="earliest"
        />
      </div>

      <div class="mt-4 flex justify-end gap-2">
        <button type="button" class="btn btn-ghost" @click="emit('cancel')">Cancel</button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!customValid"
          @click="customDate && emit('schedule', customDate)"
        >
          Schedule
        </button>
      </div>
    </div>
  </div>
</template>
