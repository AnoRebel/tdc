<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

/**
 * Consecutive messages from one person.
 *
 * Grouping keeps a run of messages from repeating the author's name on every
 * line, which is what makes a busy channel readable.
 */

interface Props {
  mine?: boolean;
  class?: HTMLAttributes["class"];
}

const props = defineProps<Props>();
</script>

<template>
  <div
    data-slot="message-group"
    :class="cn('flex flex-col gap-1', props.mine ? 'items-end' : 'items-start', props.class)"
  >
    <slot />
  </div>
</template>
