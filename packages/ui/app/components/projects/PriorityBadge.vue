<script setup lang="ts">
import type { IssuePriority } from "@tdc/types";

const { $ts } = useI18n();

/**
 * How urgent an issue is, at a glance.
 *
 * Bars fill by rank — the most urgent level of the scale shows three, the next
 * two, and anything below one — with the level's own name beside or behind
 * them, so the bars are never the only thing saying it.
 */
const props = defineProps<{ priority?: IssuePriority; compact?: boolean }>();

const filled = computed(() => {
  const rank = props.priority?.rank ?? 0;
  if (!rank) return 0;
  return rank === 1 ? 3 : rank === 2 ? 2 : 1;
});
</script>

<template>
  <span
    class="inline-flex items-center gap-1 text-xs"
    :class="props.priority?.rank === 1 ? 'text-destructive' : 'text-muted-foreground'"
  >
    <span class="flex h-3 items-end gap-px" aria-hidden="true">
      <span
        v-for="bar in 3"
        :key="bar"
        class="w-0.75 rounded-[1px]"
        :class="bar <= filled ? 'bg-current' : 'bg-border'"
        :style="{ height: `${bar * 4}px` }"
      />
    </span>
    <span :class="props.compact && 'sr-only'">
      {{ props.priority?.name ?? $ts("projects.priority.none") }}
    </span>
  </span>
</template>
