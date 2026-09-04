<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

/**
 * One message row: the bubble, and whatever sits beside it.
 *
 * The reader's own messages go right, everyone else's left. Items are aligned
 * to their ends rather than their tops so that hover actions sit level with
 * the bottom of a tall bubble instead of floating at the top of it.
 */

interface Props {
  /** True when this message was written by the person reading it. */
  mine?: boolean;
  class?: HTMLAttributes["class"];
}

const props = defineProps<Props>();
</script>

<template>
  <div
    data-slot="message"
    :class="
      cn(
        'group/row flex w-full items-end gap-1.5',
        props.mine ? 'justify-end' : 'justify-start',
        props.class,
      )
    "
  >
    <slot />
  </div>
</template>
