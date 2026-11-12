<script setup lang="ts">
import type { IssueLabel } from "@tdc/types";

/**
 * A label, as a small chip.
 *
 * The colour is a dot beside the name rather than the chip's ground or its
 * text: a project's colours are chosen for telling labels apart, not for
 * contrast, and a name printed in a pale yellow would be unreadable in the
 * light theme and the audit would be right to say so. The name carries the
 * meaning; the dot is for scanning.
 */
const props = defineProps<{ label: IssueLabel }>();
</script>

<template>
  <span
    class="inline-flex max-w-40 items-center gap-1 rounded-full border bg-surface px-1.5 py-px text-[0.6875rem] text-foreground"
  >
    <span
      class="size-2 shrink-0 rounded-full"
      :style="{ backgroundColor: props.label.colour }"
      aria-hidden="true"
    />
    <span class="truncate">{{ props.label.name }}</span>
  </span>
</template>
