<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

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
        // w-fit rather than w-full: a row stretched across the page puts the
        // hover controls at the far edge, yards from the message they act on.
        // Sizing to content keeps them beside the bubble — but max-w-full is
        // needed with it, or a long message would overflow the timeline
        // instead of wrapping.
        'group/row flex w-fit max-w-full items-end gap-1.5',
        props.mine ? 'self-end' : 'self-start',
        props.class,
      )
    "
  >
    <slot />
  </div>
</template>
