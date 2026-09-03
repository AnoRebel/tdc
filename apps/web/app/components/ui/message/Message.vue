<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

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
    :class="cn('flex w-full gap-2', props.mine ? 'justify-end' : 'justify-start', props.class)"
  >
    <slot />
  </div>
</template>
