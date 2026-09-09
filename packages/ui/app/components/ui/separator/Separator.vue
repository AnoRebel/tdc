<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

/**
 * A rule across the timeline, optionally naming what it divides.
 *
 * Used for date breaks and for the boundary between what has been read and
 * what has not. The label sits on the rule rather than above it, so the break
 * costs one line rather than three.
 */

interface Props {
  /** Text shown in the middle of the rule, such as a date. */
  label?: string;
  /**
   * Draws the rule and its label in the accent colour, for a boundary that
   * should be noticed rather than merely available — the unread marker.
   */
  accent?: boolean;
  class?: HTMLAttributes["class"];
}

const props = defineProps<Props>();
</script>

<template>
  <div
    data-slot="separator"
    role="separator"
    :aria-label="props.label"
    :class="cn('flex items-center gap-3 py-3 select-none', props.class)"
  >
    <span class="h-px flex-1" :class="props.accent ? 'bg-primary/40' : 'bg-border'" />
    <span
      v-if="props.label"
      class="text-[0.6875rem] font-medium"
      :class="props.accent ? 'text-primary' : 'text-muted-foreground'"
    >
      {{ props.label }}
    </span>
    <span
      v-if="props.label"
      class="h-px flex-1"
      :class="props.accent ? 'bg-primary/40' : 'bg-border'"
    />
  </div>
</template>
