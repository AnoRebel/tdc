<script setup lang="ts">
import type { IssueState } from "@tdc/types";

/**
 * Which state an issue is in, at a glance.
 *
 * The name is always written out. The shape beside it — a ring for work not
 * finished, a filled check for work that is — is for scanning a long list, and
 * never the only signal: somebody who cannot tell the two icons apart still
 * reads the word.
 */
const props = defineProps<{ state: IssueState; compact?: boolean }>();
</script>

<template>
  <span
    class="inline-flex max-w-full items-center gap-1.5 text-xs"
    :class="props.state.isCompletion ? 'text-success-text' : 'text-muted-foreground'"
  >
    <Icon
      v-if="props.state.isCompletion"
      name="lucide:circle-check"
      class="size-3.5 shrink-0"
      aria-hidden="true"
    />
    <Icon v-else name="lucide:circle-dashed" class="size-3.5 shrink-0" aria-hidden="true" />
    <span v-if="!props.compact" class="truncate">{{ props.state.name }}</span>
    <span v-else class="sr-only">{{ props.state.name }}</span>
  </span>
</template>
