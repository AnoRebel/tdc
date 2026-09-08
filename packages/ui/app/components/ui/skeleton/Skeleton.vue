<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

/**
 * A placeholder shaped like the content it stands in for.
 *
 * Preferred over a spinner: a shape that matches what is coming tells someone
 * what to expect and keeps the layout from jumping when it arrives.
 */
const props = defineProps<{ class?: HTMLAttributes["class"] }>();
</script>

<template>
  <div
    data-slot="skeleton"
    :class="cn('animate-pulse rounded-md bg-muted', props.class)"
    aria-hidden="true"
  />
</template>
