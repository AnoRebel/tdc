<script setup lang="ts">
import { ShieldOff } from "lucide-vue-next";

/**
 * Banning somebody from one channel.
 *
 * Asks for a reason rather than defaulting one, because the server requires it
 * and because the person banned is entitled to it — a decision nobody can
 * explain is a decision nobody can review.
 */

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
  { label: "An hour", hours: 1 },
  { label: "A day", hours: 24 },
  { label: "A week", hours: 24 * 7 },
  { label: "Permanent", hours: 0 },
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
    aria-label="Ban from channel"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div class="w-[min(26rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <ShieldOff class="size-4" />
        Ban {{ username }} from this channel
      </h2>

      <p class="mt-2 text-xs text-muted-foreground">
        They will be removed from this channel and cannot rejoin it. Everything else they belong to
        is untouched.
      </p>

      <form class="mt-4 space-y-3" @submit.prevent="submit">
        <div>
          <label for="ban-reason" class="mb-1 block text-xs font-medium"> Reason </label>
          <textarea
            id="ban-reason"
            v-model="reason"
            rows="2"
            class="input h-auto py-2"
            placeholder="What happened"
            autofocus
          />
          <p class="mt-1 text-[0.6875rem] text-muted-foreground">
            Recorded, and shown to whoever reviews this later.
          </p>
        </div>

        <fieldset>
          <legend class="mb-1.5 text-xs font-medium">How long</legend>
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
          <button type="button" class="btn btn-ghost" @click="emit('cancel')">Cancel</button>
          <button type="submit" class="btn btn-danger" :disabled="!reason.trim()">Ban</button>
        </div>
      </form>
    </div>
  </div>
</template>
