<script setup lang="ts">
import { Clock } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Choosing when to send a message.
 *
 * Presets first, because "tomorrow morning" is what somebody writing outside
 * office hours actually wants, and making them work out the date and time for
 * that is the reason scheduled sending goes unused.
 */

defineProps<{ body: string }>();

const emit = defineEmits<{ cancel: []; schedule: [sendAt: Date] }>();

/*
 * Whether the application is in its dark palette, which the picker has to be
 * told rather than being able to read.
 *
 * Dark is the default here — the light palette is opt-in through a class — so
 * the absence of that class means dark, not light.
 */
const isDark = ref(true);

onMounted(() => {
  isDark.value = !document.documentElement.classList.contains("light");
});

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

/*
 * A time of the reader's own choosing.
 *
 * A component rather than the browser's datetime input, which renders as a
 * different control on every platform — a wheel on iOS, a dropdown on Chrome,
 * an unstyled text box on Firefox — and none of them can be made to match the
 * rest of this.
 */
const custom = ref<Date | null>(null);

const customValid = computed(() => !!custom.value && custom.value.getTime() > Date.now() + 10_000);

/** The earliest the picker offers, matching what the server accepts. */
const earliest = computed(() => new Date(Date.now() + 60_000));

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
    <!-- The calendar makes this taller than a phone's screen, so the dialog is
         bounded and scrolls inside itself: the header and the buttons stay put
         while the middle moves. -->
    <div
      class="flex max-h-[calc(100dvh-2rem)] w-[min(24rem,100%)] flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
    >
      <h2
        class="flex shrink-0 items-center gap-2 border-b border-border px-4 py-3 text-sm font-semibold"
      >
        <Clock class="size-4" />
        Send later
      </h2>

      <div class="min-h-0 flex-1 overflow-y-auto p-4">
        <p
          class="max-h-16 overflow-y-auto rounded-lg bg-muted/50 px-3 py-2 text-xs text-muted-foreground"
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
          <label for="schedule-custom" class="mb-1 block text-xs font-medium">
            Or pick a time
          </label>
          <ClientOnly>
            <!-- Inline rather than a popup. A popup anchored to an input near
               the bottom of a dialog opens upward across the dialog itself,
               and no amount of stacking fixes that: the calendar needs room,
               not a higher layer. Inline, the dialog simply grows. -->
            <VueDatePicker
              v-model="custom"
              :min-date="earliest"
              :dark="isDark"
              inline
              auto-apply
              time-picker-inline
              :clearable="false"
              :enable-time-picker="true"
              :action-row="{
                showSelect: false,
                showCancel: false,
                showNow: false,
                showPreview: false,
              }"
            />

            <!-- Before the component loads, and if scripting is off entirely:
               the presets above still work, so this says so rather than
               showing a control that does nothing. -->
            <template #fallback>
              <p class="text-xs text-muted-foreground">Loading the calendar…</p>
            </template>
          </ClientOnly>
        </div>
      </div>

      <div class="flex shrink-0 justify-end gap-2 border-t border-border px-4 py-3">
        <Button variant="ghost" @click="emit('cancel')">Cancel</Button>
        <Button :disabled="!customValid" @click="custom && emit('schedule', custom)">
          Schedule
        </Button>
      </div>
    </div>
  </div>
</template>
