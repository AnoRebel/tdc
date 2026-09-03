<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

interface Props {
  /** Text shown in the middle of the rule, such as a date. */
  label?: string;
  class?: HTMLAttributes["class"];
}

const props = defineProps<Props>();
</script>

<template>
  <div
    data-slot="separator"
    role="separator"
    :class="cn('flex items-center gap-3 py-2 text-xs text-muted-foreground', props.class)"
  >
    <span class="h-px flex-1 bg-border" />
    <span v-if="props.label">{{ props.label }}</span>
    <span v-if="props.label" class="h-px flex-1 bg-border" />
  </div>
</template>
