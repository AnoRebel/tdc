<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * That the call is being recorded, and by whom — up for everybody in it for
 * as long as the recording lasts, in every size of the call window — and, for
 * the recorder, what became of their recording afterwards.
 *
 * `compact` is the minimized bar's: the mark and the word, nothing more.
 */

defineProps<{ compact?: boolean }>();
const { $t, $ts } = useI18n();
const c = useCall();
const r = useCallRecording();
const { user } = useAuth();

const recorders = computed(() => {
  const ids = c.call.value?.recording ?? [];
  return (c.call.value?.participants ?? []).filter((each) => ids.includes(each.userId));
});
const names = computed(() => recorders.value.map((each) => nameOf(each)).join(", "));
const mine = computed(() => recorders.value.some((each) => each.userId === user.value?.id));

// Said aloud when a recording begins, for whoever cannot see the mark.
const announced = ref("");
watch(names, (now, before) => {
  if (now && now !== before) announced.value = $ts("calls.record.started", { names: now });
});
const percent = computed(() =>
  r.saved.value.size ? Math.round((r.saved.value.sent / r.saved.value.size) * 100) : 0,
);
</script>

<template>
  <span class="sr-only" role="status" aria-live="polite">{{ announced }}</span>
  <span
    v-if="compact && recorders.length"
    class="flex items-center gap-1 text-[0.6875rem] font-medium text-destructive"
    :title="$ts('calls.record.by', { names })"
  >
    <span class="recording-dot" aria-hidden="true" />
    {{ $t("calls.record.indicator") }}
  </span>
  <template v-else-if="!compact">
    <p
      v-if="recorders.length"
      class="flex items-center gap-2 rounded-md bg-destructive/10 px-2 py-1 text-xs text-destructive"
    >
      <span class="recording-dot" aria-hidden="true" />
      <span class="font-medium">{{ $t("calls.record.indicator") }}</span>
      <span class="min-w-0 flex-1 truncate">
        {{ mine ? $t("calls.record.youAreRecording") : $t("calls.record.by", { names }) }}
      </span>
    </p>
    <p v-if="r.phase.value === 'starting'" class="px-1 text-xs text-muted-foreground" role="status">
      {{ $t("calls.record.starting") }}
    </p>
    <p
      v-else-if="r.phase.value === 'saving'"
      class="px-1 text-xs text-muted-foreground"
      role="status"
    >
      {{ $t("calls.record.saving", { percent }) }}
    </p>
    <p
      v-else-if="r.phase.value === 'saved'"
      class="flex items-center gap-2 px-1 text-xs text-muted-foreground"
      role="status"
    >
      <span class="flex-1">{{ $t("calls.record.saved") }}</span>
      <Button variant="ghost" size="small" class="h-6 px-2" @click="r.dismiss()">
        {{ $t("calls.record.dismiss") }}
      </Button>
    </p>
    <div
      v-else-if="r.kept.value"
      class="space-y-1 rounded-md bg-warning/10 px-2 py-1.5 text-xs text-warning"
      role="alert"
    >
      <p>{{ $t("calls.record.notKept", { reason: r.kept.value.reason }) }}</p>
      <div class="flex gap-1">
        <Button variant="outline" size="small" class="h-6 px-2" @click="r.keep()">
          {{ $t("calls.record.keep") }}
        </Button>
        <Button variant="ghost" size="small" class="h-6 px-2" @click="r.dismiss()">
          {{ $t("calls.record.discard") }}
        </Button>
      </div>
    </div>
    <p v-if="r.error.value" class="px-1 text-xs text-destructive" role="alert">
      {{ r.error.value }}
    </p>
  </template>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

.recording-dot {
  @apply size-2 shrink-0 rounded-full bg-destructive;
}
@media (prefers-reduced-motion: no-preference) {
  .recording-dot {
    @apply animate-pulse;
  }
}
</style>
