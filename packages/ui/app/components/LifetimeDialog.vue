<script setup lang="ts">
import type { MessageLifetime } from "@tdc/types";
import { Timer } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";

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

const { $t, $ts } = useI18n();

const kind = ref<MessageLifetime["kind"]>(props.current?.kind ?? "after_sending");
const seconds = ref(props.current?.seconds ?? 3600);

const kinds = computed(() => [
  {
    value: "after_sending" as const,
    label: $t("channel.lifetime.afterSending"),
    detail: $t("channel.lifetime.afterSendingHint"),
  },
  {
    value: "once_seen" as const,
    label: $t("channel.lifetime.onceSeen"),
    detail: $t("channel.lifetime.onceSeenHint"),
  },
  {
    value: "after_seeing" as const,
    label: $t("channel.lifetime.afterSeeing"),
    detail: $t("channel.lifetime.afterSeeingHint"),
  },
]);

// $ts rather than $t: these become a Select's option labels, which are typed
// as plain strings.
const durations = computed(() => [
  { seconds: 60, label: $ts("channel.lifetime.minute") },
  { seconds: 300, label: $ts("channel.lifetime.fiveMinutes") },
  { seconds: 3600, label: $ts("channel.lifetime.hour") },
  { seconds: 86400, label: $ts("channel.lifetime.day") },
  { seconds: 604800, label: $ts("channel.lifetime.week") },
]);

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
    :aria-label="$ts('channel.lifetime.title')"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div class="w-[min(26rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <Timer class="size-4" />
        {{ $t("channel.lifetime.title") }}
      </h2>

      <form class="mt-4 space-y-3" @submit.prevent="submit">
        <fieldset>
          <legend class="mb-1.5 text-xs font-medium">{{ $t("channel.lifetime.how") }}</legend>
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
          <label for="lifetime-duration" class="mb-1 block text-xs font-medium">
            {{ $t("channel.lifetime.howLong") }}
          </label>
          <Select
            v-model="seconds"
            :label="$ts('channel.lifetime.howLong')"
            :options="durations.map((choice) => ({ value: choice.seconds, label: choice.label }))"
          />
        </div>

        <p class="text-[0.6875rem] text-muted-foreground">
          {{ $t("channel.lifetime.visibleToAll") }}
        </p>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
          <Button type="submit">{{ $t("channel.lifetime.set") }}</Button>
        </div>
      </form>
    </div>
  </div>
</template>
