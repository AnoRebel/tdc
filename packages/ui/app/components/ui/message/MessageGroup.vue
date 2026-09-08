<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

/**
 * A run of messages from one person.
 *
 * Grouped so a run reads as one turn: the name and avatar appear once, and the
 * bubbles below sit tight against each other. The gap between groups is wider
 * than the gap within one, which is what separates turns without a rule.
 */

interface Props {
  /** True when the run was written by the person reading it. */
  mine?: boolean;
  class?: HTMLAttributes["class"];
}

const props = defineProps<Props>();
</script>

<template>
  <div
    data-slot="message-group"
    :class="cn('flex flex-col gap-0.5', props.mine ? 'items-end' : 'items-start', props.class)"
  >
    <slot />
  </div>
</template>
