<script setup lang="ts">
import type { MessageLifetime } from "@tdc/types";
import { Timer } from "lucide-vue-next";

/**
 * Choosing when a message expires.
 *
 * Three ways to say when, because they answer different questions. A duration
 * from sending is the simple one. Expiring once everybody has read it suits
 * something meant to be read rather than kept. A window from each person's own
 * first reading is the only one that is fair to somebody who was asleep.
 *
 * A lifetime can only be shortened after it is set, so the server refuses
 * anything longer. The dialog does not try to predict which those are — the
 * comparison is not a number comparison, and a client that guessed would
 * sometimes hide a choice the server would have accepted.
 */

const props = defineProps<{ current?: MessageLifetime }>();

const emit = defineEmits<{
  cancel: [];
  choose: [lifetime: { kind: string; seconds?: number }];
}>();

const kind = ref<MessageLifetime["kind"]>(props.current?.kind ?? "after_sending");
const seconds = ref(props.current?.seconds ?? 3600);

const kinds = [
  {
    value: "after_sending" as const,
    label: "A set time after sending",
    detail: "It goes when the time is up, whether or not anybody read it.",
  },
  {
    value: "once_seen" as const,
    label: "Once everyone has read it",
    detail: "It goes as soon as the last person opens the conversation.",
  },
  {
    value: "after_seeing" as const,
    label: "A set time after each person reads it",
    detail: "Everybody gets the same window, however long they took to look.",
  },
];

const durations = [
  { seconds: 60, label: "A minute" },
  { seconds: 300, label: "5 minutes" },
  { seconds: 3600, label: "An hour" },
  { seconds: 86400, label: "A day" },
  { seconds: 604800, label: "A week" },
];

/** Only two of the three kinds take a duration. */
const needsDuration = computed(() => kind.value !== "once_seen");

function submit() {
  emit(
    "choose",
    needsDuration.value ? { kind: kind.value, seconds: seconds.value } : { kind: kind.value },
  );
}
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    aria-label="When this message expires"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div class="w-[min(26rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <Timer class="size-4" />
        When this message expires
      </h2>

      <form class="mt-4 space-y-3" @submit.prevent="submit">
        <fieldset>
          <legend class="mb-1.5 text-xs font-medium">How</legend>
          <div class="space-y-1">
            <label
              v-for="option in kinds"
              :key="option.value"
              class="flex cursor-pointer items-start gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-accent"
            >
              <input
                v-model="kind"
                type="radio"
                :value="option.value"
                class="mt-0.5 cursor-pointer accent-primary"
              />
              <span class="min-w-0">
                <span class="block text-sm">{{ option.label }}</span>
                <span class="block text-xs text-muted-foreground">{{ option.detail }}</span>
              </span>
            </label>
          </div>
        </fieldset>

        <div v-if="needsDuration">
          <label for="lifetime-duration" class="mb-1 block text-xs font-medium">How long</label>
          <select id="lifetime-duration" v-model.number="seconds" class="input">
            <option v-for="choice in durations" :key="choice.seconds" :value="choice.seconds">
              {{ choice.label }}
            </option>
          </select>
        </div>

        <p class="text-[0.6875rem] text-muted-foreground">
          Everybody in the conversation can see that this message will expire. A lifetime can be
          made shorter afterwards, never longer.
        </p>

        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-ghost" @click="emit('cancel')">Cancel</button>
          <button type="submit" class="btn btn-primary">Set</button>
        </div>
      </form>
    </div>
  </div>
</template>
