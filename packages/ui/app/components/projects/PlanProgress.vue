<script setup lang="ts">
const { $t, $ts } = useI18n();

/**
 * How much of a cycle or milestone is done, as figures first.
 *
 * The figures are the content and the bar is decoration on them: "3 of 8
 * done" reads the same to a screen reader, in a text browser and in a
 * screenshot, and a bar alone says nothing exact to any of them.
 */
const props = defineProps<{ completed: number; total: number }>();

const percent = computed(() =>
  props.total === 0 ? 0 : Math.round((props.completed / props.total) * 100),
);
</script>

<template>
  <div class="flex items-center gap-2">
    <div
      class="h-1.5 w-24 overflow-hidden rounded-full bg-muted"
      role="progressbar"
      :aria-valuenow="props.completed"
      aria-valuemin="0"
      :aria-valuemax="props.total"
      :aria-label="$ts('projects.planning.progress')"
    >
      <div class="h-full bg-primary" :style="{ width: `${percent}%` }" />
    </div>
    <span class="text-xs text-muted-foreground tabular-nums">
      {{ $t("projects.planning.done", { completed: props.completed, total: props.total }) }}
    </span>
  </div>
</template>
