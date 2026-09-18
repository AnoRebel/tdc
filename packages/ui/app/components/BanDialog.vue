<script setup lang="ts">
import { ShieldOff } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

/**
 * Banning somebody from one channel.
 *
 * Asks for a reason rather than defaulting one, because the server requires it
 * and because the person banned is entitled to it — a decision nobody can
 * explain is a decision nobody can review.
 */

const { $t, $ts } = useI18n();

defineProps<{ username: string }>();

const emit = defineEmits<{
  cancel: [];
  ban: [reason: string, hours?: number];
}>();

const reason = ref("");

/*
 * Durations, with permanent last.
 *
 * Offered as presets because "a day" is what a moderator means, and making
 * them work out the hours is how a temporary ban becomes a permanent one by
 * accident.
 */
const durations = [
  { label: $ts("channel.lifetime.hour"), hours: 1 },
  { label: $ts("channel.lifetime.day"), hours: 24 },
  { label: $ts("channel.lifetime.week"), hours: 24 * 7 },
  { label: $ts("moderation.permanent"), hours: 0 },
];

const hours = ref(24);

function submit() {
  if (!reason.value.trim()) return;
  emit("ban", reason.value.trim(), hours.value > 0 ? hours.value : undefined);
}
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    :aria-label="$ts('moderation.banFromChannel')"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div class="w-[min(26rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <ShieldOff class="size-4" />
        {{ $t("moderation.banWhomFromChannel", { username }) }}
      </h2>

      <p class="mt-2 text-xs text-muted-foreground">
        {{ $t("moderation.banConsequenceHint") }}
      </p>

      <form class="mt-4 space-y-3" @submit.prevent="submit">
        <div>
          <label for="ban-reason" class="mb-1 block text-xs font-medium">
            {{ $t("moderation.reason") }}
          </label>
          <Textarea
            class="h-auto py-2"
            id="ban-reason"
            v-model="reason"
            rows="2"
            :placeholder="$ts('moderation.whatHappened')"
            autofocus
          />
          <p class="mt-1 text-[0.6875rem] text-muted-foreground">
            {{ $t("admin.recordedShownToReviewerHint") }}
          </p>
        </div>

        <fieldset>
          <legend class="mb-1.5 text-xs font-medium">{{ $t("moderation.howLong") }}</legend>
          <div class="flex flex-wrap gap-1">
            <button
              v-for="duration in durations"
              :key="duration.label"
              type="button"
              class="cursor-pointer rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
              :class="
                hours === duration.hours
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:bg-accent/50'
              "
              @click="hours = duration.hours"
            >
              {{ duration.label }}
            </button>
          </div>
        </fieldset>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
          <Button variant="danger" type="submit" :disabled="!reason.trim()">
            {{ $t("moderation.ban") }}
          </Button>
        </div>
      </form>
    </div>
  </div>
</template>
